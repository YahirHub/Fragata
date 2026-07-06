package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"fragata/internal/model"
)

func (s *Server) streamRecordingMP4(w http.ResponseWriter, r *http.Request, path string, start time.Duration, logKey, logValue string) {
	if !s.acquireTranscode() {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "hay demasiadas reproducciones activas; inténtelo nuevamente")
		return
	}
	defer s.releaseTranscode()

	file, info, err := openRegularRecording(path)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		http.NotFound(w, r)
		return
	}
	_ = file.Close()
	if info.Size() <= 0 {
		writeError(w, http.StatusConflict, "la grabación está vacía")
		return
	}

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-ss", formatFFmpegDuration(start),
		"-i", path,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
	}
	if strings.EqualFold(s.probeRecordingCodec(r.Context(), path), "h264") {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p")
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
	if copyErr != nil && r.Context().Err() == nil {
		s.logger.Debug("recording playback client ended", logKey, logValue, "error", copyErr)
	}
	if waitErr != nil && r.Context().Err() == nil {
		s.logger.Warn("recording playback ffmpeg failed", logKey, logValue, "error", model.RedactSecrets(stderr.String()))
	}
}

func (s *Server) probeRecordingCodec(parent context.Context, path string) string {
	if strings.TrimSpace(s.cfg.FFprobePath) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, s.cfg.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(output)))
}

func (s *Server) acquireTranscode() bool {
	select {
	case s.transcodes <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseTranscode() {
	select {
	case <-s.transcodes:
	default:
	}
}
