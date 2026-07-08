package livestream

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	FrameMetadata byte = 1
	FrameInit     byte = 2
	FrameMedia    byte = 3
	FrameError    byte = 4

	maxMP4BoxSize = 64 << 20
)

type Source struct {
	ID         string
	URL        string
	VideoCodec string
}

type Metadata struct {
	MIME      string `json:"mime"`
	Mode      string `json:"mode"`
	HasAudio  bool   `json:"has_audio"`
	Transport string `json:"transport"`
}

type Frame struct {
	Type byte
	Data []byte
}

type Manager struct {
	ffmpegPath string
	idle       time.Duration
	maxViewers int
	maxStreams int
	logger     *slog.Logger

	mu      sync.Mutex
	streams map[string]*cameraStream
	viewers int
	active  int
}

type cameraStream struct {
	manager *Manager
	source  Source
	ctx     context.Context
	cancel  context.CancelFunc

	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
	metadata    []byte
	initSegment []byte
	ready       bool
	stopped     bool
	idleTimer   *time.Timer
}

type subscriber struct {
	stream *cameraStream
	frames chan Frame
	once   sync.Once
}

type Subscription struct {
	Frames <-chan Frame
	close  func()
}

func (s *Subscription) Close() {
	if s != nil && s.close != nil {
		s.close()
	}
}

func New(ffmpegPath string, idle time.Duration, maxViewers, maxStreams int, logger *slog.Logger) *Manager {
	if idle < 10*time.Second {
		idle = 30 * time.Second
	}
	if maxViewers < 1 {
		maxViewers = 1
	}
	if maxStreams < 1 {
		maxStreams = 1
	}
	return &Manager{
		ffmpegPath: strings.TrimSpace(ffmpegPath),
		idle:       idle,
		maxViewers: maxViewers,
		maxStreams: maxStreams,
		logger:     logger,
		streams:    make(map[string]*cameraStream),
	}
}

func (m *Manager) Subscribe(ctx context.Context, source Source) (*Subscription, error) {
	if m.ffmpegPath == "" {
		return nil, errors.New("FFmpeg es necesario para la transmisión web protegida")
	}
	source.ID = strings.TrimSpace(source.ID)
	source.URL = strings.TrimSpace(source.URL)
	if source.ID == "" || source.URL == "" {
		return nil, errors.New("fuente de video incompleta")
	}

	m.mu.Lock()
	if m.viewers >= m.maxViewers {
		m.mu.Unlock()
		return nil, errors.New("se alcanzó el límite de vistas en vivo")
	}
	stream := m.streams[source.ID]
	if stream != nil && stream.source.URL != source.URL {
		stream.stopLockedByManager("la configuración de la cámara cambió")
		stream = nil
	}
	if stream == nil {
		if m.active >= m.maxStreams {
			m.mu.Unlock()
			return nil, errors.New("se alcanzó el límite de cámaras transmitidas simultáneamente")
		}
		streamCtx, cancel := context.WithCancel(context.Background())
		stream = &cameraStream{
			manager:     m,
			source:      source,
			ctx:         streamCtx,
			cancel:      cancel,
			subscribers: make(map[*subscriber]struct{}),
		}
		m.streams[source.ID] = stream
		m.active++
		go stream.run()
	}
	m.viewers++
	m.mu.Unlock()

	sub := &subscriber{stream: stream, frames: make(chan Frame, 8)}
	stream.mu.Lock()
	if stream.stopped {
		stream.mu.Unlock()
		m.releaseViewer()
		return nil, errors.New("la transmisión se reinició; vuelva a intentarlo")
	}
	if stream.idleTimer != nil {
		stream.idleTimer.Stop()
		stream.idleTimer = nil
	}
	stream.subscribers[sub] = struct{}{}
	if stream.ready {
		sub.frames <- Frame{Type: FrameMetadata, Data: append([]byte(nil), stream.metadata...)}
		sub.frames <- Frame{Type: FrameInit, Data: append([]byte(nil), stream.initSegment...)}
	}
	stream.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			sub.close()
		case <-stream.ctx.Done():
		}
	}()

	return &Subscription{Frames: sub.frames, close: sub.close}, nil
}

func (m *Manager) ViewerCount(cameraID string) int {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return 0
	}
	m.mu.Lock()
	stream := m.streams[cameraID]
	if stream == nil {
		m.mu.Unlock()
		return 0
	}
	stream.mu.Lock()
	count := len(stream.subscribers)
	stream.mu.Unlock()
	m.mu.Unlock()
	return count
}

func (m *Manager) releaseViewer() { m.releaseViewers(1) }

func (m *Manager) releaseViewers(count int) {
	if count < 1 {
		return
	}
	m.mu.Lock()
	m.viewers -= count
	if m.viewers < 0 {
		m.viewers = 0
	}
	m.mu.Unlock()
}

func (s *subscriber) close() {
	s.once.Do(func() {
		stream := s.stream
		removed := false
		stream.mu.Lock()
		if _, ok := stream.subscribers[s]; ok {
			delete(stream.subscribers, s)
			close(s.frames)
			removed = true
		}
		if len(stream.subscribers) == 0 && !stream.stopped {
			stream.idleTimer = time.AfterFunc(stream.manager.idle, func() {
				stream.stopIfIdle()
			})
		}
		stream.mu.Unlock()
		if removed {
			stream.manager.releaseViewer()
		}
	})
}

func (s *cameraStream) stopIfIdle() {
	s.mu.Lock()
	if len(s.subscribers) != 0 || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	s.cancel()
	s.manager.removeStream(s)
}

func (s *cameraStream) stopLockedByManager(reason string) {
	// manager.mu debe estar retenido por el llamador.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	for sub := range s.subscribers {
		message := []byte(reason)
		select {
		case sub.frames <- Frame{Type: FrameError, Data: message}:
		default:
		}
		close(sub.frames)
		delete(s.subscribers, sub)
		sub.once.Do(func() {})
		if s.manager.viewers > 0 {
			s.manager.viewers--
		}
	}
	s.mu.Unlock()
	s.cancel()
	delete(s.manager.streams, s.source.ID)
	if s.manager.active > 0 {
		s.manager.active--
	}
}

func (m *Manager) removeStream(s *cameraStream) {
	m.mu.Lock()
	if current := m.streams[s.source.ID]; current == s {
		delete(m.streams, s.source.ID)
		if m.active > 0 {
			m.active--
		}
	}
	m.mu.Unlock()
}

func (s *cameraStream) run() {
	err := s.runFFmpeg()
	if s.ctx.Err() != nil {
		return
	}
	if err == nil {
		err = errors.New("FFmpeg terminó inesperadamente")
	}
	if s.manager.logger != nil {
		s.manager.logger.Warn("live HTTP stream stopped", "camera_id", s.source.ID, "error", redactURL(err.Error()))
	}
	s.fail(err)
}

func (s *cameraStream) runFFmpeg() error {
	args, mode := ffmpegArgs(s.source)
	if s.manager.logger != nil {
		s.manager.logger.Info("starting protected live stream", "camera_id", s.source.ID, "mode", mode)
	}
	cmd := exec.CommandContext(s.ctx, s.manager.ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("abrir salida de FFmpeg: %w", err)
	}
	stderr := &tailBuffer{limit: 24 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("iniciar FFmpeg: %w", err)
	}

	parseErr := s.readMP4(stdout, mode)
	waitErr := cmd.Wait()
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if parseErr != nil && !errors.Is(parseErr, io.EOF) && !errors.Is(parseErr, io.ErrUnexpectedEOF) {
		return parseErr
	}
	message := strings.TrimSpace(stderr.String())
	if waitErr != nil {
		if message != "" {
			return fmt.Errorf("FFmpeg terminó: %v: %s", waitErr, redactURL(message))
		}
		return fmt.Errorf("FFmpeg terminó: %w", waitErr)
	}
	if message != "" {
		return fmt.Errorf("FFmpeg terminó: %s", redactURL(message))
	}
	return parseErr
}

func ffmpegArgs(source Source) ([]string, string) {
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-timeout", "15000000",
		"-fflags", "+genpts+discardcorrupt+nobuffer",
		"-flags", "low_delay",
		"-analyzeduration", "2000000",
		"-probesize", "2000000",
		"-i", source.URL,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-sn", "-dn",
	}
	mode := "copy"
	if strings.EqualFold(strings.TrimSpace(source.VideoCodec), "H264") {
		args = append(args, "-c:v", "copy")
	} else {
		mode = "transcode"
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-bf", "0",
			"-g", "30",
			"-keyint_min", "15",
			"-sc_threshold", "0",
			"-force_key_frames", "expr:gte(t,n_forced*1)",
			"-x264-params", "repeat-headers=1:aud=1",
		)
	}
	args = append(args,
		"-c:a", "aac",
		"-b:a", "96k",
		"-ac", "2",
		"-ar", "48000",
		"-max_interleave_delta", "1000000",
		"-avoid_negative_ts", "make_zero",
		"-f", "mp4",
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof+omit_tfhd_offset",
		"-flush_packets", "1",
		"pipe:1",
	)
	return args, mode
}

func (s *cameraStream) readMP4(reader io.Reader, mode string) error {
	var initSegment bytes.Buffer
	var fragment bytes.Buffer
	var prefix bytes.Buffer
	ready := false

	for {
		boxType, box, err := readMP4Box(reader)
		if err != nil {
			return err
		}
		if !ready {
			if boxType != "moof" {
				_, _ = initSegment.Write(box)
				continue
			}
			metadata, err := metadataFromInit(initSegment.Bytes(), mode)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(metadata)
			if err != nil {
				return err
			}
			s.setReady(encoded, initSegment.Bytes())
			if s.manager.logger != nil {
				s.manager.logger.Info("protected live stream ready", "camera_id", s.source.ID, "mode", mode, "audio", metadata.HasAudio)
			}
			ready = true
			_, _ = fragment.Write(box)
			continue
		}

		switch boxType {
		case "moof":
			fragment.Reset()
			if prefix.Len() > 0 {
				_, _ = fragment.Write(prefix.Bytes())
				prefix.Reset()
			}
			_, _ = fragment.Write(box)
		case "mdat":
			if fragment.Len() == 0 {
				continue
			}
			_, _ = fragment.Write(box)
			s.broadcast(fragment.Bytes())
			fragment.Reset()
		default:
			if fragment.Len() > 0 {
				_, _ = fragment.Write(box)
			} else {
				_, _ = prefix.Write(box)
			}
		}
	}
}

func readMP4Box(reader io.Reader) (string, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", nil, err
	}
	size := uint64(binary.BigEndian.Uint32(header[:4]))
	headerSize := uint64(8)
	if size == 1 {
		extended := make([]byte, 8)
		if _, err := io.ReadFull(reader, extended); err != nil {
			return "", nil, err
		}
		header = append(header, extended...)
		size = binary.BigEndian.Uint64(extended)
		headerSize = 16
	}
	if size == 0 {
		return "", nil, errors.New("FFmpeg produjo una caja MP4 sin tamaño definido")
	}
	if size < headerSize || size > maxMP4BoxSize {
		return "", nil, fmt.Errorf("caja MP4 inválida de %d bytes", size)
	}
	payload := make([]byte, int(size-headerSize))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", nil, err
	}
	box := append(header, payload...)
	return string(header[4:8]), box, nil
}

func metadataFromInit(initSegment []byte, mode string) (Metadata, error) {
	avc := bytes.Index(initSegment, []byte("avcC"))
	if avc < 0 || avc+8 > len(initSegment) {
		return Metadata{}, errors.New("FFmpeg no produjo configuración H.264 compatible")
	}
	// Después de 'avcC': configurationVersion, profile, compatibility y level.
	profile := initSegment[avc+5]
	compatibility := initSegment[avc+6]
	level := initSegment[avc+7]
	videoCodec := fmt.Sprintf("avc1.%02X%02X%02X", profile, compatibility, level)
	hasAudio := bytes.Contains(initSegment, []byte("mp4a"))
	codecs := videoCodec
	if hasAudio {
		codecs += ", mp4a.40.2"
	}
	return Metadata{
		MIME:      fmt.Sprintf("video/mp4; codecs=\"%s\"", codecs),
		Mode:      mode,
		HasAudio:  hasAudio,
		Transport: "http-fmp4",
	}, nil
}

func (s *cameraStream) setReady(metadata, initSegment []byte) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.metadata = append([]byte(nil), metadata...)
	s.initSegment = append([]byte(nil), initSegment...)
	s.ready = true
	for sub := range s.subscribers {
		sub.frames <- Frame{Type: FrameMetadata, Data: append([]byte(nil), metadata...)}
		sub.frames <- Frame{Type: FrameInit, Data: append([]byte(nil), initSegment...)}
	}
	s.mu.Unlock()
}

func (s *cameraStream) broadcast(data []byte) {
	payload := append([]byte(nil), data...)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	var slow []*subscriber
	for sub := range s.subscribers {
		select {
		case sub.frames <- Frame{Type: FrameMedia, Data: payload}:
		default:
			slow = append(slow, sub)
		}
	}
	released := 0
	for _, sub := range slow {
		delete(s.subscribers, sub)
		select {
		case sub.frames <- Frame{Type: FrameError, Data: []byte("el navegador no consumió el video a tiempo")}:
		default:
		}
		close(sub.frames)
		sub.once.Do(func() { released++ })
	}
	if len(s.subscribers) == 0 && !s.stopped && s.idleTimer == nil {
		s.idleTimer = time.AfterFunc(s.manager.idle, func() { s.stopIfIdle() })
	}
	s.mu.Unlock()
	s.manager.releaseViewers(released)
}

func (s *cameraStream) fail(err error) {
	message := []byte(redactURL(err.Error()))
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	released := 0
	for sub := range s.subscribers {
		select {
		case sub.frames <- Frame{Type: FrameError, Data: message}:
		default:
		}
		close(sub.frames)
		delete(s.subscribers, sub)
		sub.once.Do(func() { released++ })
	}
	s.mu.Unlock()
	s.manager.releaseViewers(released)
	s.cancel()
	s.manager.removeStream(s)
}

type tailBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.limit <= 0 {
		return original, nil
	}
	if len(p) >= b.limit {
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[len(p)-b.limit:])
		return original, nil
	}
	if b.buffer.Len()+len(p) > b.limit {
		current := b.buffer.Bytes()
		keep := b.limit - len(p)
		copyOfTail := append([]byte(nil), current[len(current)-keep:]...)
		b.buffer.Reset()
		_, _ = b.buffer.Write(copyOfTail)
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *tailBuffer) String() string { return b.buffer.String() }

func redactURL(value string) string {
	if at := strings.Index(value, "@"); at >= 0 {
		if scheme := strings.LastIndex(value[:at], "://"); scheme >= 0 {
			return value[:scheme+3] + "***:***" + value[at:]
		}
	}
	return value
}
