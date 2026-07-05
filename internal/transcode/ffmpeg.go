package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
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
// rebuilds complete H.264 access units in a Fragata hub. Recording never passes
// through FFmpeg; the original maximum-quality stream remains untouched.
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
		"-pix_fmt", "yuv420p", "-profile:v", "baseline", "-x264-params", "repeat-headers=1:aud=1",
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

	generation := s.Hub.BeginSource()
	defer s.Hub.EndSource(generation)
	assembler := newH264RTPAssembler(s.Hub, generation, s.Width, s.Height)
	buffer := make([]byte, 64<<10)
	lastPacket := time.Now()
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
			lastPacket = time.Now()
			if s.OnPacket != nil {
				s.OnPacket(n)
			}
			s.Hub.PublishRTP(&packet)
			assembler.Push(&packet)
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

type h264RTPAssembler struct {
	hub           *stream.Hub
	generation    uint64
	width         int
	height        int
	haveTimestamp bool
	timestamp     uint32
	baseTimestamp uint32
	haveBase      bool
	haveSequence  bool
	lastSequence  uint16
	nalus         [][]byte
	fragment      []byte
	sps           []byte
	pps           []byte
}

func newH264RTPAssembler(hub *stream.Hub, generation uint64, width, height int) *h264RTPAssembler {
	return &h264RTPAssembler{hub: hub, generation: generation, width: width, height: height}
}

func (a *h264RTPAssembler) Push(packet *rtp.Packet) {
	if packet == nil || len(packet.Payload) == 0 {
		return
	}
	if a.haveSequence && packet.SequenceNumber != a.lastSequence+1 {
		a.fragment = nil
	}
	a.haveSequence = true
	a.lastSequence = packet.SequenceNumber

	if a.haveTimestamp && packet.Timestamp != a.timestamp {
		a.flush()
	}
	if !a.haveTimestamp {
		a.haveTimestamp = true
		a.timestamp = packet.Timestamp
		if !a.haveBase {
			a.baseTimestamp = packet.Timestamp
			a.haveBase = true
		}
	}

	payload := packet.Payload
	switch payload[0] & 0x1f {
	case 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23:
		a.appendNALU(payload)
	case 24: // STAP-A
		for offset := 1; offset+2 <= len(payload); {
			size := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
			offset += 2
			if size <= 0 || offset+size > len(payload) {
				break
			}
			a.appendNALU(payload[offset : offset+size])
			offset += size
		}
	case 28: // FU-A
		if len(payload) < 3 {
			break
		}
		start := payload[1]&0x80 != 0
		end := payload[1]&0x40 != 0
		naluType := payload[1] & 0x1f
		if start {
			a.fragment = append(a.fragment[:0], (payload[0]&0xe0)|naluType)
			a.fragment = append(a.fragment, payload[2:]...)
		} else if len(a.fragment) > 0 {
			a.fragment = append(a.fragment, payload[2:]...)
		}
		if end && len(a.fragment) > 0 {
			a.appendNALU(a.fragment)
			a.fragment = nil
		}
	}
	if packet.Marker {
		a.flush()
	}
}

func (a *h264RTPAssembler) appendNALU(nalu []byte) {
	if len(nalu) == 0 {
		return
	}
	copyNALU := append([]byte(nil), nalu...)
	switch copyNALU[0] & 0x1f {
	case 7:
		a.sps = append(a.sps[:0], copyNALU...)
	case 8:
		a.pps = append(a.pps[:0], copyNALU...)
	}
	a.nalus = append(a.nalus, copyNALU)
}

func (a *h264RTPAssembler) flush() {
	if !a.haveTimestamp {
		return
	}
	if len(a.nalus) == 0 {
		a.nalus = nil
		a.fragment = nil
		a.haveTimestamp = false
		return
	}
	keyFrame := false
	for _, nalu := range a.nalus {
		if len(nalu) > 0 && nalu[0]&0x1f == 5 {
			keyFrame = true
			break
		}
	}
	a.hub.SetInfo(stream.Info{
		Codec: "H264", Width: a.width, Height: a.height,
		SPS: append([]byte(nil), a.sps...), PPS: append([]byte(nil), a.pps...),
	})
	delta := a.timestamp - a.baseTimestamp
	pts := time.Duration(delta) * time.Second / 90000
	a.hub.PublishAccessUnit(stream.AccessUnit{
		PTS: pts, NALUs: a.nalus, KeyFrame: keyFrame, Generation: a.generation,
	})
	a.nalus = nil
	a.fragment = nil
	a.haveTimestamp = false
}
