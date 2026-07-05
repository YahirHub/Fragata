package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fragata/internal/model"
)

const eventPlaybackContext = 5 * time.Second

type publicDetectionEvent struct {
	model.DetectionEvent
	SnapshotURL         string  `json:"snapshot_url,omitempty"`
	DetailURL           string  `json:"detail_url"`
	PlaybackURL         string  `json:"playback_url,omitempty"`
	RecordingURL        string  `json:"recording_url,omitempty"`
	RecordingAvailable  bool    `json:"recording_available"`
	RecordingPending    bool    `json:"recording_pending"`
	PlaybackSupported   bool    `json:"playback_supported"`
	PlaybackOffsetSecs  float64 `json:"playback_offset_seconds,omitempty"`
	PlaybackContextSecs float64 `json:"playback_context_seconds,omitempty"`
	CameraWidth         int     `json:"camera_width,omitempty"`
	CameraHeight        int     `json:"camera_height,omitempty"`
}

type resolvedEventRecording struct {
	absolute  string
	relative  string
	startedAt time.Time
	offset    time.Duration
	pending   bool
}

func (s *Server) publicDetectionEvent(event model.DetectionEvent) publicDetectionEvent {
	item := publicDetectionEvent{DetectionEvent: event}
	item.SnapshotPath = ""
	item.RecordingPath = ""
	item.DetailURL = "/events/" + url.PathEscape(event.ID)
	if event.SnapshotPath != "" {
		item.SnapshotURL = "/api/events/" + url.PathEscape(event.ID) + "/snapshot"
	}
	if camera, ok := s.cameras.Camera(event.CameraID); ok {
		item.CameraWidth = camera.Width
		item.CameraHeight = camera.Height
	}
	resolved, ok := s.resolveEventRecording(event)
	if !ok {
		return item
	}
	item.RecordingStartedAt = resolved.startedAt
	item.RecordingOffsetMillis = resolved.offset.Milliseconds()
	item.PlaybackOffsetSecs = resolved.offset.Seconds()
	item.RecordingPending = resolved.pending
	item.RecordingAvailable = !resolved.pending
	if resolved.pending {
		return item
	}
	item.RecordingURL = "/api/events/" + url.PathEscape(event.ID) + "/recording"
	item.PlaybackSupported = s.cfg.FFmpegPath != ""
	if item.PlaybackSupported {
		item.PlaybackURL = "/api/events/" + url.PathEscape(event.ID) + "/video"
		contextDuration := eventPlaybackContext
		if resolved.offset < contextDuration {
			contextDuration = resolved.offset
		}
		item.PlaybackContextSecs = contextDuration.Seconds()
	}
	return item
}

func (s *Server) getDetectionEvent(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	event, ok := s.store.DetectionEvent(id)
	if !ok {
		writeError(w, http.StatusNotFound, "evento no encontrado")
		return
	}
	writeJSON(w, http.StatusOK, s.publicDetectionEvent(event))
}

func (s *Server) detectionEventRecording(w http.ResponseWriter, r *http.Request) {
	event, ok := s.store.DetectionEvent(strings.TrimSpace(r.PathValue("id")))
	if !ok {
		http.NotFound(w, r)
		return
	}
	resolved, ok := s.resolveEventRecording(event)
	if !ok || resolved.pending {
		writeError(w, http.StatusConflict, "la grabación todavía no está disponible")
		return
	}
	file, info, err := openRegularRecording(resolved.absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "no se pudo abrir la grabación")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(resolved.absolute)))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, filepath.Base(resolved.absolute), info.ModTime(), file)
}

func (s *Server) detectionEventVideo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.FFmpegPath == "" {
		writeError(w, http.StatusServiceUnavailable, "la reproducción web no está disponible")
		return
	}
	event, ok := s.store.DetectionEvent(strings.TrimSpace(r.PathValue("id")))
	if !ok {
		http.NotFound(w, r)
		return
	}
	resolved, ok := s.resolveEventRecording(event)
	if !ok || resolved.pending {
		writeError(w, http.StatusConflict, "la grabación todavía no está disponible")
		return
	}
	probeFile, info, err := openRegularRecording(resolved.absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "no se pudo abrir la grabación")
		return
	}
	_ = probeFile.Close()
	if info.Size() == 0 {
		writeError(w, http.StatusConflict, "la grabación está vacía")
		return
	}

	start := resolved.offset - eventPlaybackContext
	if start < 0 {
		start = 0
	}
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-ss", formatFFmpegDuration(start),
		"-i", resolved.absolute,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
	}
	camera, _ := s.cameras.Camera(event.CameraID)
	if strings.EqualFold(camera.Codec, "H264") {
		args = append(args, "-c:v", "copy")
	} else {
		// Keep the source dimensions. Only the codec changes when the browser
		// cannot decode the original H.265 recording.
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p")
	}
	args = append(args,
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4", "pipe:1",
	)

	cmd := exec.CommandContext(r.Context(), s.cfg.FFmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo preparar la reproducción")
		return
	}
	stderr := &boundedBuffer{limit: 16 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo iniciar la reproducción")
		return
	}
	defer func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	}()

	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, stdout)
	waitErr := cmd.Wait()
	if copyErr != nil && !errors.Is(copyErr, r.Context().Err()) {
		s.logger.Debug("event playback client ended", "event_id", event.ID, "error", copyErr)
	}
	if waitErr != nil && r.Context().Err() == nil {
		s.logger.Warn("event playback ffmpeg failed", "event_id", event.ID, "error", model.RedactSecrets(stderr.String()))
	}
}

func (s *Server) resolveEventRecording(event model.DetectionEvent) (resolvedEventRecording, bool) {
	if strings.TrimSpace(event.RecordingPath) != "" {
		startedAt := event.RecordingStartedAt
		offset := time.Duration(event.RecordingOffsetMillis) * time.Millisecond
		if offset <= 0 && !startedAt.IsZero() && event.CreatedAt.After(startedAt) {
			offset = event.CreatedAt.Sub(startedAt)
		}
		if resolved, ok := s.resolveRecordingPath(event.RecordingPath, startedAt, offset); ok {
			return resolved, true
		}
	}
	return s.findRecordingForEvent(event)
}

func (s *Server) resolveRecordingPath(relative string, startedAt time.Time, offset time.Duration) (resolvedEventRecording, bool) {
	absolute, ok := safeRecordingPath(s.cfg.RecordingsDir, relative)
	if !ok {
		return resolvedEventRecording{}, false
	}
	if info, err := os.Stat(absolute); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return resolvedEventRecording{absolute: absolute, relative: filepath.ToSlash(relative), startedAt: startedAt, offset: maxDuration(offset, 0)}, true
	}
	partial := absolute + ".partial"
	if info, err := os.Stat(partial); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return resolvedEventRecording{absolute: partial, relative: filepath.ToSlash(relative), startedAt: startedAt, offset: maxDuration(offset, 0), pending: true}, true
	}
	if strings.HasSuffix(strings.ToLower(absolute), ".mkv") {
		recovered := strings.TrimSuffix(absolute, filepath.Ext(absolute)) + ".recovered.mkv"
		if info, err := os.Stat(recovered); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return resolvedEventRecording{absolute: recovered, relative: filepath.ToSlash(relative), startedAt: startedAt, offset: maxDuration(offset, 0)}, true
		}
	}
	return resolvedEventRecording{}, false
}

func (s *Server) findRecordingForEvent(event model.DetectionEvent) (resolvedEventRecording, bool) {
	camera, ok := s.cameras.Camera(event.CameraID)
	if !ok || strings.TrimSpace(camera.FolderName) == "" {
		return resolvedEventRecording{}, false
	}
	eventLocal := event.CreatedAt.In(time.Local)
	maxWindow := time.Duration(camera.SegmentDurationSeconds)*time.Second + 2*time.Minute
	if maxWindow < 3*time.Minute {
		maxWindow = 3 * time.Minute
	}
	var best resolvedEventRecording
	found := false
	for dayDelta := -1; dayDelta <= 1; dayDelta++ {
		day := eventLocal.AddDate(0, 0, dayDelta)
		dir := filepath.Join(s.cfg.RecordingsDir, camera.FolderName, day.Format("2006"), day.Format("01"), day.Format("02"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			lower := strings.ToLower(entry.Name())
			pending := strings.HasSuffix(lower, ".mkv.partial")
			if !pending && !strings.HasSuffix(lower, ".mkv") {
				continue
			}
			startedAt, ok := recordingStartFromName(day, entry.Name())
			if !ok || startedAt.After(eventLocal) {
				continue
			}
			offset := eventLocal.Sub(startedAt)
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				continue
			}
			if pending {
				if offset > maxWindow {
					continue
				}
			} else if eventLocal.After(info.ModTime().Add(10 * time.Second)) {
				continue
			}
			absolute := filepath.Join(dir, entry.Name())
			relative, err := filepath.Rel(s.cfg.RecordingsDir, absolute)
			if err != nil {
				continue
			}
			if pending {
				relative = strings.TrimSuffix(relative, ".partial")
			}
			candidate := resolvedEventRecording{absolute: absolute, relative: filepath.ToSlash(relative), startedAt: startedAt.UTC(), offset: offset, pending: pending}
			if !found || candidate.startedAt.After(best.startedAt) {
				best = candidate
				found = true
			}
		}
	}
	return best, found
}

func recordingStartFromName(day time.Time, name string) (time.Time, bool) {
	base := strings.TrimSuffix(name, ".partial")
	base = strings.TrimSuffix(base, ".mkv")
	base = strings.TrimSuffix(base, ".recovered")
	if len(base) < len("15-04-05.000") {
		return time.Time{}, false
	}
	clock, err := time.ParseInLocation("15-04-05.000", base[:len("15-04-05.000")], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(), time.Local), true
}

func safeRecordingPath(root, relative string) (string, bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relative)))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", false
	}
	absolute, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil || (absolute != rootAbs && !strings.HasPrefix(absolute, rootAbs+string(os.PathSeparator))) {
		return "", false
	}
	return absolute, true
}

func openRegularRecording(path string) (*os.File, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = errors.New("la grabación no es un archivo regular")
		}
		return nil, nil, err
	}
	return file, info, nil
}

func formatFFmpegDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	return strconv.FormatFloat(value.Seconds(), 'f', 3, 64)
}

func maxDuration(value, minimum time.Duration) time.Duration {
	if value < minimum {
		return minimum
	}
	return value
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
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
		if keep < 0 {
			keep = 0
		}
		tail := append([]byte(nil), current[len(current)-keep:]...)
		b.buffer.Reset()
		_, _ = b.buffer.Write(tail)
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *boundedBuffer) String() string { return b.buffer.String() }
