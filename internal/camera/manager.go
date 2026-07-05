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
	"fragata/internal/detection"
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
		if cam.SegmentDurationSeconds != stored.SegmentDurationSeconds || cam.FolderName != stored.FolderName {
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
	if cam.Upload && (m.uploader == nil || !m.uploader.Enabled(cam.SFTPProfileID)) {
		return model.Camera{}, errors.New("seleccione un servidor SFTP habilitado antes de activar las subidas")
	}
	id, err := randomID()
	if err != nil {
		return model.Camera{}, err
	}
	now := time.Now().UTC()
	cam.ID = id
	folder, err := m.uniqueFolderName(cam.FolderName, cam.Name, "")
	if err != nil {
		return model.Camera{}, err
	}
	cam.FolderName = folder
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
	sftpProfileID := current.SFTPProfileID
	segmentDurationSeconds := current.SegmentDurationSeconds
	detectionEnabled, detectMotion, detectPerson := current.DetectionEnabled, current.DetectMotion, current.DetectPerson
	motionSensitivity, detectionInterval, personConfidence, detectionCooldown := current.MotionSensitivity, current.DetectionIntervalSecs, current.PersonConfidence, current.DetectionCooldownSecs
	detectionZone, snapshotURL := current.DetectionZone, current.SnapshotURL
	detected, err := Detect(ctx, m.cfg, AddRequest{
		Name: current.Name, Host: current.Host, Username: current.Username, Password: current.Password,
		Enabled: &enabled, Record: &record, Upload: &upload, SFTPProfileID: sftpProfileID,
	})
	if err != nil {
		return DetectionResult{}, err
	}
	updated := detected.Camera
	updated.ID = current.ID
	updated.CreatedAt = current.CreatedAt
	updated.SegmentDurationSeconds = segmentDurationSeconds
	updated.FolderName = current.FolderName
	updated.SFTPProfileID = current.SFTPProfileID
	if updated.SnapshotURL == "" {
		updated.SnapshotURL = snapshotURL
	}
	updated.DetectionEnabled = detectionEnabled
	updated.DetectMotion = detectMotion
	updated.DetectPerson = detectPerson
	updated.MotionSensitivity = motionSensitivity
	updated.DetectionIntervalSecs = detectionInterval
	updated.PersonConfidence = personConfidence
	updated.DetectionCooldownSecs = detectionCooldown
	updated.DetectionZone = detectionZone
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

func (m *Manager) LiveAudioHub(ctx context.Context, id string) (*stream.Hub, string, error) {
	m.mu.RLock()
	w, ok := m.workers[id]
	m.mu.RUnlock()
	if !ok {
		return nil, "", errors.New("cámara no encontrada o deshabilitada")
	}
	hub, mode, err := w.ensureLive(ctx)
	if err != nil {
		return nil, mode, err
	}
	if err := w.ensureLiveAudio(ctx, hub); err != nil {
		return nil, mode, err
	}
	return hub, mode, nil
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
	w := &worker{cam: cam, cfg: m.cfg, store: m.store, uploader: m.uploader, logger: m.logger, hub: stream.NewHub(), ctx: ctx, cancel: cancel, done: make(chan struct{})}
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
	if cam.FolderName == "" {
		folder, err := normalizeFolderName(cam.ID, "camera")
		if err == nil {
			cam.FolderName = folder
		}
	}
	if cam.SegmentDurationSeconds < model.MinSegmentDurationSeconds || cam.SegmentDurationSeconds > model.MaxSegmentDurationSeconds {
		seconds := int64(m.cfg.SegmentDuration / time.Second)
		if seconds < model.MinSegmentDurationSeconds || seconds > model.MaxSegmentDurationSeconds {
			seconds = 5 * 60
		}
		cam.SegmentDurationSeconds = seconds
	}
	if cam.MotionSensitivity < 1 || cam.MotionSensitivity > 100 {
		cam.MotionSensitivity = 65
	}
	if cam.DetectionIntervalSecs < 1 || cam.DetectionIntervalSecs > 60 {
		cam.DetectionIntervalSecs = 1
	}
	if cam.PersonConfidence < 40 || cam.PersonConfidence > 95 {
		cam.PersonConfidence = 55
	}
	if cam.DetectionCooldownSecs < 1 || cam.DetectionCooldownSecs > 3600 {
		cam.DetectionCooldownSecs = 30
	}
	cam.DetectionZone = cam.DetectionZone.Normalized()
	if !cam.DetectMotion && !cam.DetectPerson {
		cam.DetectMotion = true
	}
	return cam
}

type worker struct {
	cam             model.Camera
	cfg             config.Config
	store           *store.Store
	uploader        *upload.Uploader
	logger          *slog.Logger
	hub             *stream.Hub
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
	detectionDone   chan struct{}
	mu              sync.RWMutex
	status          model.RuntimeStatus
	recordingMu     sync.Mutex
	recordingCancel context.CancelFunc
	recordingDone   chan struct{}
	segmentDuration atomic.Int64
	liveMu          sync.Mutex
	liveHub         *stream.Hub
	liveContext     context.Context
	liveCancel      context.CancelFunc
	liveDone        chan struct{}
	liveAudioCancel context.CancelFunc
	liveAudioDone   chan struct{}
	liveAudioError  string
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
	if w.cam.DetectionEnabled {
		w.detectionDone = make(chan struct{})
		go w.runDetection()
		defer func() { w.cancel(); <-w.detectionDone }()
	} else {
		defer w.cancel()
	}
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

func (w *worker) runDetection() {
	defer close(w.detectionDone)
	runner := detection.Runner{
		Camera: w.cam, DataDir: w.cfg.DataDir, Store: w.store, Logger: w.logger,
		RecordingSnapshot: func(at time.Time) (string, time.Time, bool) {
			w.mu.RLock()
			path := w.status.RecordingPath
			startedAt := w.status.RecordingStarted
			w.mu.RUnlock()
			if strings.TrimSpace(path) == "" || startedAt.IsZero() || at.Before(startedAt) {
				return "", time.Time{}, false
			}
			return path, startedAt, true
		},
		OnStatus: func(state string, score float64, eventType string, at time.Time) {
			w.update(func(status *model.RuntimeStatus) {
				status.DetectionState = state
				status.LastMotionScore = score
				if eventType != "" {
					status.LastEventType = eventType
				}
				if !at.IsZero() {
					status.LastDetectionAt = at
				}
			})
		},
	}
	if err := runner.Run(w.ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.update(func(status *model.RuntimeStatus) { status.DetectionState = "error" })
		w.logger.Warn("camera detection stopped", "camera_id", w.cam.ID, "error", model.RedactSecrets(err.Error()))
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
			CameraID: w.cam.ID, StorageFolder: w.cam.FolderName, BaseDir: w.cfg.RecordingsDir, Hub: w.hub,
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
				if w.cam.Upload && w.uploader != nil && w.uploader.Enabled(w.cam.SFTPProfileID) {
					relative, err := filepath.Rel(w.cfg.RecordingsDir, file.Path)
					if err == nil {
						if err := w.uploader.Enqueue(w.cam.ID, w.cam.SFTPProfileID, file.Path, relative); err != nil {
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
	primaryH264 := strings.EqualFold(w.cam.Codec, "H264")
	if primaryH264 && w.cfg.FFmpegPath == "" {
		if err := waitForH264(ctx, w.hub, func() string { return "" }); err != nil {
			return nil, "", err
		}
		w.update(func(status *model.RuntimeStatus) { status.LiveMode = "direct" })
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
		w.liveContext = liveCtx
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
			stopAudioBridge := w.bridgeLiveAudio(ctx, hub)
			raw, credentialErr := fragrtsp.WithCredentials(w.cam.RTSPURL, w.cam.Username, w.cam.Password)
			if credentialErr != nil {
				err = credentialErr
			} else {
				err = (transcode.FFmpegSource{
					Path: w.cfg.FFmpegPath, URL: raw, Width: w.cam.Width, Height: w.cam.Height, Hub: hub, NoPacketTimeout: 10 * time.Second,
				}).Run(ctx)
			}
			stopAudioBridge()
			if err != nil && strings.EqualFold(w.cam.Codec, "H264") {
				w.logger.Warn("FFmpeg live view failed; using direct H264 stream", "camera_id", w.cam.ID, "error", model.RedactSecrets(err.Error()))
				mode = "direct"
				backoff = time.Second
				continue
			}
			if err != nil && strings.EqualFold(w.cam.LiveCodec, "H264") && w.cam.LiveRTSPURL != "" {
				w.logger.Warn("FFmpeg live view failed; using H264 substream", "camera_id", w.cam.ID, "error", model.RedactSecrets(err.Error()))
				mode = "substream"
				backoff = time.Second
				continue
			}
		case "direct":
			raw, credentialErr := fragrtsp.WithCredentials(w.cam.RTSPURL, w.cam.Username, w.cam.Password)
			if credentialErr != nil {
				err = credentialErr
			} else {
				err = (&fragrtsp.Source{
					URL: raw, Width: w.cam.Width, Height: w.cam.Height, Hub: hub,
				}).Run(ctx)
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
		w.stopLiveAudioProcess()
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

func (w *worker) bridgeLiveAudio(ctx context.Context, destination *stream.Hub) func() {
	bridgeCtx, cancel := context.WithCancel(ctx)
	packets, unsubscribe := w.hub.SubscribeAudio(512)
	go func() {
		defer unsubscribe()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-bridgeCtx.Done():
				return
			case <-ticker.C:
				// FFmpeg inicia una nueva generación de video y limpia el hub.
				// Solo se replica audio que WebRTC puede enviar sin convertir.
				if info := w.hub.AudioInfo(); browserAudioReady(info) {
					destination.SetAudioInfo(info)
				}
			case packet, ok := <-packets:
				if !ok {
					return
				}
				info := w.hub.AudioInfo()
				if !browserAudioReady(info) {
					continue
				}
				destination.SetAudioInfo(info)
				destination.PublishAudio(packet)
			}
		}
	}()
	return cancel
}

func (w *worker) ensureLiveAudio(ctx context.Context, destination *stream.Hub) error {
	if browserAudioReady(destination.AudioInfo()) {
		return nil
	}
	sourceAudio := w.hub.AudioInfo()
	if sourceAudio.Codec == "" {
		return errors.New("la cámara no ofrece una pista de audio compatible")
	}
	if browserAudioReady(sourceAudio) {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			if browserAudioReady(destination.AudioInfo()) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return errors.New("el audio de la cámara todavía no está disponible")
			case <-ticker.C:
			}
		}
	}
	if sourceAudio.Codec != "AAC" {
		return errors.New("el codec de audio no es compatible con el navegador")
	}
	if w.cfg.FFmpegPath == "" {
		return errors.New("el audio AAC requiere FFmpeg para reproducirse en el navegador")
	}

	w.liveMu.Lock()
	if w.liveContext == nil || w.liveHub != destination {
		w.liveMu.Unlock()
		return errors.New("la vista en vivo se reinició")
	}
	if w.liveAudioDone == nil {
		raw, err := fragrtsp.WithCredentials(w.cam.RTSPURL, w.cam.Username, w.cam.Password)
		if err != nil {
			w.liveMu.Unlock()
			return err
		}
		audioCtx, cancel := context.WithCancel(w.liveContext)
		done := make(chan struct{})
		w.liveAudioCancel = cancel
		w.liveAudioDone = done
		w.liveAudioError = ""
		go w.runAACLiveAudio(audioCtx, raw, destination, done)
	}
	done := w.liveAudioDone
	w.liveLastRequest = time.Now()
	w.liveMu.Unlock()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if browserAudioReady(destination.AudioInfo()) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			w.liveMu.Lock()
			message := w.liveAudioError
			w.liveMu.Unlock()
			if message == "" {
				message = "el audio terminó antes de quedar disponible"
			}
			return errors.New(message)
		case <-ticker.C:
		}
	}
}

func (w *worker) runAACLiveAudio(ctx context.Context, rawURL string, destination *stream.Hub, done chan struct{}) {
	err := (transcode.FFmpegAudioSource{
		Path: w.cfg.FFmpegPath, URL: rawURL, Hub: destination, NoPacketTimeout: 15 * time.Second,
	}).Run(ctx)
	message := ""
	if err != nil && !errors.Is(err, context.Canceled) {
		message = model.RedactSecrets(err.Error())
		w.logger.Warn("FFmpeg audio unavailable; video remains active", "camera_id", w.cam.ID, "error", message)
	}
	w.liveMu.Lock()
	if w.liveAudioDone == done {
		w.liveAudioCancel = nil
		w.liveAudioDone = nil
		w.liveAudioError = message
	}
	w.liveMu.Unlock()
	close(done)
}

func browserAudioReady(info stream.AudioInfo) bool {
	return info.Codec == "PCMA" || info.Codec == "PCMU" || info.Codec == "OPUS"
}

func (w *worker) stopLiveAudioProcess() {
	w.liveMu.Lock()
	cancel := w.liveAudioCancel
	done := w.liveAudioDone
	w.liveAudioCancel = nil
	w.liveAudioDone = nil
	w.liveAudioError = ""
	w.liveMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
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
	if w.liveAudioCancel != nil {
		w.liveAudioCancel()
	}
	w.liveHub = nil
	w.liveContext = nil
	w.liveCancel = nil
	w.liveDone = nil
	w.liveAudioCancel = nil
	w.liveAudioDone = nil
	w.liveAudioError = ""
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
	audio := w.hub.AudioInfo()
	out.AudioCodec = audio.Codec
	out.AudioSampleRate = audio.SampleRate
	out.AudioChannels = audio.Channels
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
