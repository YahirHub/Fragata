package transcode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"fragata/internal/model"
	"fragata/internal/stream"
	"github.com/pion/rtp"
)

// FFmpegSource converts a camera video stream to browser-compatible H.264 and
// publishes its RTP packets into a Fragata hub. Recording never passes through
// FFmpeg; the original maximum-quality stream remains untouched.
type FFmpegSource struct {
	Path            string
	URL             string
	Width           int
	Height          int
	Hub             *stream.Hub
	OnPacket        func(size int)
	NoPacketTimeout time.Duration
}

func (s FFmpegSource) Run(ctx context.Context) error {
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
		return fmt.Errorf("abrir receptor RTP local: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(2 << 20)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	outputURL := fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", port)

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-analyzeduration", "1000000", "-probesize", "1000000",
		"-i", s.URL,
		"-map", "0:v:0", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-profile:v", "baseline", "-x264-params", "repeat-headers=1",
		"-bf", "0", "-g", "50", "-keyint_min", "25", "-sc_threshold", "0",
		"-f", "rtp", "-payload_type", "96", outputURL,
	}
	cmd := exec.CommandContext(ctx, s.Path, args...)
	cmd.Stdout = io.Discard
	stderr := &tailBuffer{limit: 16 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("iniciar FFmpeg: %w", err)
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

	buffer := make([]byte, 64<<10)
	lastPacket := time.Now()
	started := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		n, _, readErr := conn.ReadFromUDP(buffer)
		if readErr == nil {
			var packet rtp.Packet
			if err := packet.Unmarshal(buffer[:n]); err != nil {
				continue
			}
			if !started {
				s.Hub.SetInfo(stream.Info{Codec: "H264", Width: s.Width, Height: s.Height})
				started = true
			}
			lastPacket = time.Now()
			if s.OnPacket != nil {
				s.OnPacket(n)
			}
			s.Hub.PublishRTP(&packet)
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
			return fmt.Errorf("leer RTP de FFmpeg: %w", readErr)
		}
		if time.Since(lastPacket) >= s.NoPacketTimeout {
			return fmt.Errorf("FFmpeg no entregó video H.264 durante %s", s.NoPacketTimeout)
		}
	}
}

func listenRTP() (*net.UDPConn, error) {
	for range 16 {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		if err != nil {
			return nil, err
		}
		if conn.LocalAddr().(*net.UDPAddr).Port%2 == 0 {
			return conn, nil
		}
		_ = conn.Close()
	}
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
}

func ffmpegExitError(err error, stderr string) error {
	message := strings.TrimSpace(model.RedactSecrets(stderr))
	if message == "" {
		message = "sin detalle"
	}
	if err == nil {
		return fmt.Errorf("FFmpeg terminó inesperadamente: %s", message)
	}
	return fmt.Errorf("FFmpeg terminó: %v: %s", err, message)
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
		if keep < 0 {
			keep = 0
		}
		copyOfTail := append([]byte(nil), current[len(current)-keep:]...)
		b.buffer.Reset()
		_, _ = b.buffer.Write(copyOfTail)
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *tailBuffer) String() string { return b.buffer.String() }
