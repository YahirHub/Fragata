package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"fragata/internal/stream"
	"github.com/pion/rtp"
)

// FFmpegAudioSource converts only the camera audio to PCMU for browsers. It is
// intentionally independent from FFmpegSource: audio failure must never stop a
// healthy video stream.
type FFmpegAudioSource struct {
	Path            string
	URL             string
	Hub             *stream.Hub
	NoPacketTimeout time.Duration
}

func (s FFmpegAudioSource) Run(ctx context.Context) error {
	if strings.TrimSpace(s.Path) == "" {
		return errors.New("FFmpeg no está disponible")
	}
	if s.Hub == nil {
		return errors.New("hub de salida requerido")
	}
	if strings.TrimSpace(s.URL) == "" {
		return errors.New("URL RTSP requerida para FFmpeg")
	}
	if s.NoPacketTimeout <= 0 {
		s.NoPacketTimeout = 15 * time.Second
	}

	conn, err := listenRTP()
	if err != nil {
		return fmt.Errorf("abrir receptor RTP de audio local: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(512 << 10)
	outputURL := fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", conn.LocalAddr().(*net.UDPAddr).Port)
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-analyzeduration", "1000000", "-probesize", "1000000",
		"-i", s.URL,
		"-map", "0:a:0", "-vn",
		"-c:a", "pcm_mulaw", "-ar", "8000", "-ac", "1",
		"-f", "rtp", "-payload_type", "0", outputURL,
	}
	cmd := exec.CommandContext(ctx, s.Path, args...)
	cmd.Stdout = io.Discard
	stderr := &tailBuffer{limit: 16 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("iniciar FFmpeg para audio: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	reaped := false
	defer func() {
		if reaped {
			return
		}
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}()

	s.Hub.SetAudioInfo(stream.AudioInfo{Codec: "PCMU", SampleRate: 8000, Channels: 1})
	buffer := make([]byte, 16<<10)
	var baseTimestamp uint32
	haveBase := false
	lastPacket := time.Now()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, readErr := conn.ReadFromUDP(buffer)
		if readErr == nil {
			var packet rtp.Packet
			if err := packet.Unmarshal(buffer[:n]); err != nil {
				continue
			}
			lastPacket = time.Now()
			if !haveBase {
				baseTimestamp = packet.Timestamp
				haveBase = true
			}
			delta := packet.Timestamp - baseTimestamp
			s.Hub.PublishAudio(stream.AudioPacket{
				PTS: time.Duration(delta) * time.Second / 8000, Payload: append([]byte(nil), packet.Payload...),
				RTP: packet.Clone(),
			})
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case waitErr := <-waitCh:
			reaped = true
			return ffmpegExitError(waitErr, stderr.String())
		default:
		}
		if netErr, ok := readErr.(net.Error); !ok || !netErr.Timeout() {
			return fmt.Errorf("leer RTP de audio de FFmpeg: %w", readErr)
		}
		if time.Since(lastPacket) >= s.NoPacketTimeout {
			return fmt.Errorf("FFmpeg no entregó audio durante %s", s.NoPacketTimeout)
		}
	}
}
