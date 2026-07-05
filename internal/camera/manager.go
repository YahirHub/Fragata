package camera

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/recording"
	fragrtsp "fragata/internal/rtsp"
	"fragata/internal/store"
	"fragata/internal/stream"
	"fragata/internal/upload"
)

type Manager struct {
	cfg      config.Config
	store    *store.Store
	uploader *upload.Uploader
	logger   *slog.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
	workers  map[string]*worker
}

func NewManager(cfg config.Config, state *store.Store, uploader *upload.Uploader, logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{cfg: cfg, store: state, uploader: uploader, logger: logger, ctx: ctx, cancel: cancel, workers: make(map[string]*worker)}
}

func (m *Manager) Start() {
	for _, cam := range m.store.Cameras() {
		if cam.Enabled {
			m.startWorker(cam)
		}
	}
}

func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	workers := make([]*worker, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	m.workers = make(map[string]*worker)
	m.mu.Unlock()
	for _, w := range workers {
		w.stop()
	}
}

func (m *Manager) Add(cam model.Camera) (model.Camera, error) {
	id, err := randomID()
	if err != nil {
		return model.Camera{}, err
	}
	now := time.Now().UTC()
	cam.ID = id
	cam.CreatedAt = now
	cam.UpdatedAt = now
	if err := m.store.SaveCamera(cam); err != nil {
		return model.Camera{}, err
	}
	if cam.Enabled {
		m.startWorker(cam)
	}
	return cam, nil
}

func (m *Manager) Delete(id string) error {
	cam, exists := m.store.Camera(id)
	if !exists {
		return errors.New("cámara no encontrada")
	}
	m.mu.Lock()
	w := m.workers[id]
	delete(m.workers, id)
	m.mu.Unlock()
	if w != nil {
		w.stop()
	}
	if err := m.store.DeleteCamera(id); err != nil {
		if cam.Enabled {
			m.startWorker(cam)
		}
		return err
	}
	return nil
}

func (m *Manager) Cameras() []model.CameraPublic {
	cameras := m.store.Cameras()
	out := make([]model.CameraPublic, 0, len(cameras))
	for _, cam := range cameras {
		out = append(out, cam.Public())
	}
	return out
}

func (m *Manager) Camera(id string) (model.Camera, bool) { return m.store.Camera(id) }

func (m *Manager) Hub(id string) (*stream.Hub, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workers[id]
	if !ok {
		return nil, false
	}
	return w.hub, true
}

func (m *Manager) Statuses() []model.RuntimeStatus {
	m.mu.RLock()
	workers := make([]*worker, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	m.mu.RUnlock()
	out := make([]model.RuntimeStatus, 0, len(workers))
	for _, w := range workers {
		out = append(out, w.statusSnapshot())
	}
	return out
}

func (m *Manager) startWorker(cam model.Camera) {
	m.mu.Lock()
	if _, exists := m.workers[cam.ID]; exists {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	w := &worker{cam: cam, cfg: m.cfg, uploader: m.uploader, logger: m.logger, hub: stream.NewHub(), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	w.status = model.RuntimeStatus{CameraID: cam.ID, State: "starting", Codec: cam.Codec}
	m.workers[cam.ID] = w
	m.mu.Unlock()
	go w.run()
}

type worker struct {
	cam      model.Camera
	cfg      config.Config
	uploader *upload.Uploader
	logger   *slog.Logger
	hub      *stream.Hub
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.RWMutex
	status   model.RuntimeStatus
}

func (w *worker) run() {
	defer close(w.done)
	defer w.hub.Close()
	recorderDone := make(chan struct{})
	if w.cam.Record {
		go func() {
			defer close(recorderDone)
			recorder := recording.Recorder{
				CameraID: w.cam.ID, BaseDir: w.cfg.RecordingsDir, SegmentDuration: w.cfg.SegmentDuration, Hub: w.hub,
				OnStarted: func(recordingPath string, started time.Time) {
					relative, err := filepath.Rel(w.cfg.RecordingsDir, recordingPath)
					if err != nil {
						relative = filepath.Base(recordingPath)
					}
					w.update(func(s *model.RuntimeStatus) {
						s.RecordingPath = filepath.ToSlash(relative)
						s.RecordingStarted = started
					})
				},
				OnCompleted: func(file recording.CompletedFile) {
					w.update(func(s *model.RuntimeStatus) { s.RecordingPath = ""; s.RecordingStarted = time.Time{} })
					if w.cam.Upload && w.uploader != nil && w.uploader.Enabled() {
						relative, err := filepath.Rel(w.cfg.RecordingsDir, file.Path)
						if err == nil {
							if err := w.uploader.Enqueue(w.cam.ID, file.Path, relative); err != nil {
								w.setError(err)
							}
						}
					}
				},
				OnError: w.setError,
			}
			if err := recorder.Run(w.ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.setError(err)
			}
		}()
	} else {
		close(recorderDone)
	}
	defer func() { <-recorderDone }()

	backoff := 2 * time.Second
	for w.ctx.Err() == nil {
		w.update(func(s *model.RuntimeStatus) { s.State = "connecting" })
		raw, err := fragrtsp.WithCredentials(w.cam.RTSPURL, w.cam.Username, w.cam.Password)
		if err != nil {
			w.setError(err)
			return
		}
		var receivedPacket atomic.Bool
		source := fragrtsp.Source{
			URL: raw, Width: w.cam.Width, Height: w.cam.Height, Hub: w.hub,
			OnPacket: func(size int) {
				now := time.Now().UTC()
				firstPacket := receivedPacket.CompareAndSwap(false, true)
				w.update(func(s *model.RuntimeStatus) {
					if firstPacket {
						s.State = "online"
						s.ConnectedAt = now
						s.LastError = ""
					}
					s.LastPacketAt = now
					s.PacketsReceived++
					s.BytesReceived += uint64(size)
					if firstPacket {
						s.Codec = w.cam.Codec
					}
				})
			},
		}
		err = source.Run(w.ctx)
		if receivedPacket.Load() {
			backoff = 2 * time.Second
		}
		if w.ctx.Err() != nil {
			break
		}
		w.setError(err)
		w.update(func(s *model.RuntimeStatus) { s.State = "reconnecting" })
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (w *worker) stop() {
	w.cancel()
	<-w.done
}

func (w *worker) setError(err error) {
	if err == nil {
		return
	}
	safeMessage := model.RedactSecrets(err.Error())
	w.logger.Warn("camera stream error", "camera_id", w.cam.ID, "error", safeMessage)
	w.update(func(s *model.RuntimeStatus) { s.LastError = safeMessage })
}

func (w *worker) update(fn func(*model.RuntimeStatus)) {
	w.mu.Lock()
	fn(&w.status)
	w.mu.Unlock()
}

func (w *worker) statusSnapshot() model.RuntimeStatus {
	w.mu.RLock()
	out := w.status
	w.mu.RUnlock()
	out.Viewers = w.hub.ViewerCount()
	return out
}

func randomID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generar ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
