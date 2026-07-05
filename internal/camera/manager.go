package camera

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/recording"
	fragrtsp "fragata/internal/rtsp"
	"fragata/internal/store"
	"fragata/internal/stream"
	"fragata/internal/transcode"
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
	for _, stored := range m.store.Cameras() {
		cam := m.normalizeCamera(stored)
		if cam.SegmentDurationSeconds != stored.SegmentDurationSeconds {
			if err := m.store.SaveCamera(cam); err != nil {
				m.logger.Warn("could not persist default segment duration", "camera_id", cam.ID, "error", err)
			}
		}
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
	cam = m.normalizeCamera(cam)
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
		out = append(out, m.normalizeCamera(cam).Public())
	}
	return out
}

func (m *Manager) Camera(id string) (model.Camera, bool) {
	cam, ok := m.store.Camera(id)
	if !ok {
		return model.Camera{}, false
	}
	return m.normalizeCamera(cam), true
}

type CameraSettings struct {
	Record                 *bool
	SegmentDurationSeconds *int64
}

func (m *Manager) UpdateSettings(id string, settings CameraSettings) (model.Camera, error) {
	cam, exists := m.store.Camera(id)
	if !exists {
		return model.Camera{}, errors.New("cámara no encontrada")
	}
	cam = m.normalizeCamera(cam)
	changed := false
	if settings.Record != nil && cam.Record != *settings.Record {
		cam.Record = *settings.Record
		changed = true
	}
	if settings.SegmentDurationSeconds != nil {
		seconds := *settings.SegmentDurationSeconds
		if seconds < model.MinSegmentDurationSeconds || seconds > model.MaxSegmentDurationSeconds {
			return model.Camera{}, fmt.Errorf("la duración por archivo debe estar entre %d minuto y %d horas", model.MinSegmentDurationSeconds/60, model.MaxSegmentDurationSeconds/3600)
		}
		if seconds%60 != 0 {
			return model.Camera{}, errors.New("la duración por archivo debe configurarse en minutos completos")
		}
		if cam.SegmentDurationSeconds != seconds {
			cam.SegmentDurationSeconds = seconds
			changed = true
		}
	}
	if !changed {
		return cam, nil
	}
	cam.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCamera(cam); err != nil {
		return model.Camera{}, err
	}
	m.mu.RLock()
	w := m.workers[id]
	m.mu.RUnlock()
	if w != nil {
		w.configureRecording(cam.Record, time.Duration(cam.SegmentDurationSeconds)*time.Second)
	}
	return cam, nil
}

func (m *Manager) UpdateRecording(id string, enabled bool) (model.Camera, error) {
	return m.UpdateSettings(id, CameraSettings{Record: &enabled})
}

func (m *Manager) Redetect(ctx context.Context, id string) (DetectionResult, error) {
	current, exists := m.store.Camera(id)
	if !exists {
		return DetectionResult{}, errors.New("cámara no encontrada")
	}
	current = m.normalizeCamera(current)
	enabled, record, upload := current.Enabled, current.Record, current.Upload
	segmentDurationSeconds := current.SegmentDurationSeconds
	detected, err := Detect(ctx, m.cfg, AddRequest{
		Name: current.Name, Host: current.Host, Username: current.Username, Password: current.Password,
		Enabled: &enabled, Record: &record, Upload: &upload,
	})
	if err != nil {
		return DetectionResult{}, err
	}
	updated := detected.Camera
	updated.ID = current.ID
	updated.CreatedAt = current.CreatedAt
	updated.SegmentDurationSeconds = segmentDurationSeconds
	updated.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCamera(updated); err != nil {
		return DetectionResult{}, err
	}
	m.restartWorker(updated)
	detected.Camera = updated
	return detected, nil
}

func (m *Manager) Hub(id string) (*stream.Hub, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workers[id]
	if !ok {
		return nil, false
	}
	return w.hub, true
}

func (m *Manager) LiveHub(ctx context.Context, id string) (*stream.Hub, string, error) {
	m.mu.RLock()
	w, ok := m.workers[id]
	m.mu.RUnlock()
	if !ok {
		return nil, "", errors.New("cámara no encontrada o deshabilitada")
	}
	return w.ensureLive(ctx)
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
	cam = m.normalizeCamera(cam)
	w := &worker{cam: cam, cfg: m.cfg, uploader: m.uploader, logger: m.logger, hub: stream.NewHub(), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	w.segmentDuration.Store(int64(time.Duration(cam.SegmentDurationSeconds) * time.Second))
	w.status = model.RuntimeStatus{CameraID: cam.ID, State: "starting", Codec: cam.Codec}
	m.workers[cam.ID] = w
	m.mu.Unlock()
	go w.run()
}

func (m *Manager) restartWorker(cam model.Camera) {
	m.mu.Lock()
	w := m.workers[cam.ID]
	delete(m.workers, cam.ID)
	m.mu.Unlock()
	if w != nil {
		w.stop()
	}
	if cam.Enabled {
		m.startWorker(cam)
	}
}

func (m *Manager) normalizeCamera(cam model.Camera) model.Camera {
	if cam.SegmentDurationSeconds < model.MinSegmentDurationSeconds || cam.SegmentDurationSeconds > model.MaxSegmentDurationSeconds {
		seconds := int64(m.cfg.SegmentDuration / time.Second)
		if seconds < model.MinSegmentDurationSeconds || seconds > model.MaxSegmentDurationSeconds {
			seconds = 5 * 60
		}
		cam.SegmentDurationSeconds = seconds
	}
	return cam
}

type worker struct {
	cam             model.Camera
	cfg             config.Config
	uploader        *upload.Uploader
	logger          *slog.Logger
	hub             *stream.Hub
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
	mu              sync.RWMutex
	status          model.RuntimeStatus
	recordingMu     sync.Mutex
	recordingCancel context.CancelFunc
	recordingDone   chan struct{}
	segmentDuration atomic.Int64
	liveMu          sync.Mutex
	liveHub         *stream.Hub
	liveCancel      context.CancelFunc
	liveDone        chan struct{}
	liveStarted     bool
	liveMode        string
	liveError       string
	liveLastRequest time.Time
}

func (w *worker) run() {
	defer close(w.done)
	defer w.hub.Close()
	defer w.stopRecorder()
	defer w.stopLive()
	if w.cam.Record {
		w.startRecorder()
	}

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

func (w *worker) configureRecording(enabled bool, duration time.Duration) {
	if duration > 0 {
		w.segmentDuration.Store(int64(duration))
	}
	if enabled {
		w.startRecorder()
		return
	}
	w.stopRecorder()
}

func (w *worker) startRecorder() {
	w.recordingMu.Lock()
	if w.recordingCancel != nil {
		w.recordingMu.Unlock()
		return
	}
	recorderCtx, cancel := context.WithCancel(w.ctx)
	done := make(chan struct{})
	w.recordingCancel = cancel
	w.recordingDone = done
	w.recordingMu.Unlock()

	go func() {
		defer close(done)
		recorder := recording.Recorder{
			CameraID: w.cam.ID, BaseDir: w.cfg.RecordingsDir, Hub: w.hub,
			SegmentDurationProvider: func() time.Duration {
				return time.Duration(w.segmentDuration.Load())
			},
			OnStarted: func(recordingPath string, started time.Time) {
				relative, err := filepath.Rel(w.cfg.RecordingsDir, recordingPath)
				if err != nil {
					relative = filepath.Base(recordingPath)
				}
				w.update(func(status *model.RuntimeStatus) {
					status.RecordingPath = filepath.ToSlash(relative)
					status.RecordingStarted = started
				})
			},
			OnCompleted: func(file recording.CompletedFile) {
				w.update(func(status *model.RuntimeStatus) {
					if status.RecordingStarted.Equal(file.StartedAt) {
						status.RecordingPath = ""
						status.RecordingStarted = time.Time{}
					}
				})
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
		if err := recorder.Run(recorderCtx); err != nil && !errors.Is(err, context.Canceled) {
			w.setError(err)
		}
		w.recordingMu.Lock()
		if w.recordingDone == done {
			w.recordingCancel = nil
			w.recordingDone = nil
		}
		w.recordingMu.Unlock()
	}()
}

func (w *worker) stopRecorder() {
	w.recordingMu.Lock()
	cancel := w.recordingCancel
	done := w.recordingDone
	w.recordingMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	w.recordingMu.Lock()
	if w.recordingDone == done {
		w.recordingCancel = nil
		w.recordingDone = nil
	}
	w.recordingMu.Unlock()
}

func (w *worker) stop() {
	w.cancel()
	<-w.done
}

func (w *worker) ensureLive(ctx context.Context) (*stream.Hub, string, error) {
	if strings.EqualFold(w.cam.Codec, "H264") {
		w.update(func(status *model.RuntimeStatus) { status.LiveMode = "direct" })
		if err := waitForH264(ctx, w.hub, func() string { return "" }); err != nil {
			return nil, "", err
		}
		return w.hub, "direct", nil
	}

	w.liveMu.Lock()
	if w.liveStarted && channelClosed(w.liveDone) {
		w.resetLiveLocked()
	}
	w.liveLastRequest = time.Now()
	if !w.liveStarted {
		mode := ""
		switch {
		case w.cfg.FFmpegPath != "":
			mode = "ffmpeg"
		case strings.EqualFold(w.cam.LiveCodec, "H264") && w.cam.LiveRTSPURL != "":
			mode = "substream"
		default:
			w.liveMu.Unlock()
			return nil, "", errors.New("la cámara usa H.265 y no hay FFmpeg ni un substream H.264 disponible para el navegador")
		}
		liveCtx, cancel := context.WithCancel(w.ctx)
		w.liveHub = stream.NewHub()
		w.liveCancel = cancel
		done := make(chan struct{})
		w.liveDone = done
		w.liveStarted = true
		w.liveMode = mode
		w.liveError = ""
		liveHub := w.liveHub
		go func() {
			go w.watchLiveIdle(liveCtx, cancel, liveHub)
			w.runLive(liveCtx, mode, liveHub)
			cancel()
			cleared := false
			w.liveMu.Lock()
			if w.liveDone == done {
				w.resetLiveLocked()
				cleared = true
			}
			w.liveMu.Unlock()
			if cleared {
				w.update(func(status *model.RuntimeStatus) { status.LiveMode = "" })
			}
			close(done)
		}()
	}
	hub := w.liveHub
	mode := w.liveMode
	w.liveMu.Unlock()

	if err := waitForH264(ctx, hub, w.currentLiveError); err != nil {
		return nil, mode, err
	}
	w.liveMu.Lock()
	mode = w.liveMode
	w.liveMu.Unlock()
	return hub, mode, nil
}

func (w *worker) runLive(ctx context.Context, initialMode string, hub *stream.Hub) {
	defer hub.Close()
	mode := initialMode
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		w.setLiveState(mode, "")
		var err error
		switch mode {
		case "ffmpeg":
			raw, credentialErr := fragrtsp.WithCredentials(w.cam.RTSPURL, w.cam.Username, w.cam.Password)
			if credentialErr != nil {
				err = credentialErr
			} else {
				err = (transcode.FFmpegSource{
					Path: w.cfg.FFmpegPath, URL: raw, Width: w.cam.Width, Height: w.cam.Height, Hub: hub, NoPacketTimeout: 10 * time.Second,
				}).Run(ctx)
			}
			if err != nil && strings.EqualFold(w.cam.LiveCodec, "H264") && w.cam.LiveRTSPURL != "" {
				w.logger.Warn("FFmpeg live view failed; using H264 substream", "camera_id", w.cam.ID, "error", model.RedactSecrets(err.Error()))
				mode = "substream"
				backoff = time.Second
				continue
			}
		case "substream":
			raw, credentialErr := fragrtsp.WithCredentials(w.cam.LiveRTSPURL, w.cam.Username, w.cam.Password)
			if credentialErr != nil {
				err = credentialErr
			} else {
				err = (&fragrtsp.Source{
					URL: raw, Width: w.cam.LiveWidth, Height: w.cam.LiveHeight, Hub: hub,
				}).Run(ctx)
			}
		default:
			err = errors.New("modo de vista en vivo inválido")
		}
		if ctx.Err() != nil {
			return
		}
		w.setLiveState(mode, errString(err))
		select {
		case <-ctx.Done():
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

func (w *worker) setLiveState(mode, errorMessage string) {
	w.liveMu.Lock()
	w.liveMode = mode
	w.liveError = model.RedactSecrets(errorMessage)
	w.liveMu.Unlock()
	w.update(func(status *model.RuntimeStatus) { status.LiveMode = mode })
}

func (w *worker) currentLiveError() string {
	w.liveMu.Lock()
	defer w.liveMu.Unlock()
	return w.liveError
}

func (w *worker) stopLive() {
	w.liveMu.Lock()
	cancel := w.liveCancel
	done := w.liveDone
	w.liveMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	w.liveMu.Lock()
	if w.liveDone == done {
		w.resetLiveLocked()
	}
	w.liveMu.Unlock()
}

func (w *worker) watchLiveIdle(ctx context.Context, cancel context.CancelFunc, hub *stream.Hub) {
	idleTimeout := w.cfg.LiveIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Second
	}
	interval := 5 * time.Second
	if idleTimeout < interval {
		interval = idleTimeout / 2
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastActive := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if hub.ViewerCount() > 0 {
				lastActive = now
				continue
			}
			w.liveMu.Lock()
			lastRequest := w.liveLastRequest
			w.liveMu.Unlock()
			if lastRequest.After(lastActive) {
				lastActive = lastRequest
			}
			if now.Sub(lastActive) >= idleTimeout {
				cancel()
				return
			}
		}
	}
}

func (w *worker) resetLiveLocked() {
	w.liveHub = nil
	w.liveCancel = nil
	w.liveDone = nil
	w.liveStarted = false
	w.liveMode = ""
	w.liveError = ""
	w.liveLastRequest = time.Time{}
}

func channelClosed(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func waitForH264(ctx context.Context, hub *stream.Hub, lastError func() string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info := hub.Info(); strings.EqualFold(info.Codec, "H264") {
			return nil
		}
		select {
		case <-ctx.Done():
			if message := strings.TrimSpace(lastError()); message != "" {
				return fmt.Errorf("la vista en vivo no pudo iniciar: %s", message)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func errString(err error) string {
	if err == nil {
		return "el proceso de vista terminó inesperadamente"
	}
	return err.Error()
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
	w.liveMu.Lock()
	if w.liveHub != nil {
		out.Viewers = w.liveHub.ViewerCount()
	}
	if w.liveMode != "" {
		out.LiveMode = w.liveMode
	}
	w.liveMu.Unlock()
	return out
}

func randomID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generar ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
