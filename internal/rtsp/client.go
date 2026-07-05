package rtsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"fragata/internal/stream"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/pion/rtp"
)

type ProbeResult struct {
	URL   string `json:"url"`
	Codec string `json:"codec"`
}

func WithCredentials(raw, username, password string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return "", errors.New("la URL debe usar rtsp:// o rtsps://")
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String(), nil
}

func WithoutCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func ExtractCredentials(raw string) (username, password string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return "", "", false
	}
	username = u.User.Username()
	password, _ = u.User.Password()
	return username, password, true
}

func NormalizeHost(raw, host string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	port := u.Port()
	if port == "" {
		port = "554"
		if u.Scheme == "rtsps" {
			port = "322"
		}
	}
	// Una cámara no debe poder redirigir la sonda a otro host mediante una URI ONVIF.
	u.Host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	return u.String()
}

func Probe(ctx context.Context, rawURL string, timeout time.Duration) (ProbeResult, error) {
	u, err := base.ParseURL(rawURL)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("URL RTSP inválida: %w", err)
	}
	protocol := gortsplib.ProtocolTCP
	c := &gortsplib.Client{
		Scheme: u.Scheme, Host: u.Host, Protocol: &protocol, ReadTimeout: timeout, WriteTimeout: timeout,
	}
	if err := c.Start(); err != nil {
		return ProbeResult{}, fmt.Errorf("conectar TCP RTSP a %s: %w", u.Host, err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	defer close(done)

	desc, _, err := c.Describe(u)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("DESCRIBE RTSP: %w", err)
	}

	var (
		media *description.Media
		forma any
		codec string
	)
	var h264 *format.H264
	if found := desc.FindFormat(&h264); found != nil {
		media, forma, codec = found, h264, "H264"
	} else {
		var h265 *format.H265
		if found := desc.FindFormat(&h265); found != nil {
			media, forma, codec = found, h265, "H265"
		}
	}
	if media == nil {
		return ProbeResult{}, errors.New("el stream no contiene video H.264 ni H.265")
	}
	if _, err := c.Setup(desc.BaseURL, media, 0, 0); err != nil {
		return ProbeResult{}, fmt.Errorf("SETUP RTSP: %w", err)
	}
	packets := make(chan struct{}, 1)
	switch typed := forma.(type) {
	case *format.H264:
		c.OnPacketRTP(media, typed, func(*rtp.Packet) {
			select {
			case packets <- struct{}{}:
			default:
			}
		})
	case *format.H265:
		c.OnPacketRTP(media, typed, func(*rtp.Packet) {
			select {
			case packets <- struct{}{}:
			default:
			}
		})
	}
	if _, err := c.Play(nil); err != nil {
		return ProbeResult{}, fmt.Errorf("PLAY RTSP: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ProbeResult{}, ctx.Err()
	case <-timer.C:
		return ProbeResult{}, errors.New("el stream RTSP respondió pero no entregó video")
	case <-packets:
		return ProbeResult{URL: WithoutCredentials(rawURL), Codec: codec}, nil
	}
}

func CommonCandidates(host string) []string {
	candidates := ExpandCandidates(host, []int{554}, nil, 96)
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.URL)
	}
	return out
}

type Source struct {
	URL      string
	Width    int
	Height   int
	Hub      *stream.Hub
	OnPacket func(size int)
}

func (s *Source) Run(ctx context.Context) error {
	u, err := base.ParseURL(s.URL)
	if err != nil {
		return fmt.Errorf("URL RTSP inválida: %w", err)
	}
	protocol := gortsplib.ProtocolTCP
	c := &gortsplib.Client{
		Scheme: u.Scheme, Host: u.Host, Protocol: &protocol, ReadTimeout: 15 * time.Second, WriteTimeout: 10 * time.Second,
	}
	if err := c.Start(); err != nil {
		return fmt.Errorf("conectar RTSP: %w", err)
	}
	defer c.Close()

	desc, _, err := c.Describe(u)
	if err != nil {
		return fmt.Errorf("DESCRIBE RTSP: %w", err)
	}

	var h264 *format.H264
	if media := desc.FindFormat(&h264); media != nil {
		return s.runH264(ctx, c, desc.BaseURL, media, h264)
	}
	var h265 *format.H265
	if media := desc.FindFormat(&h265); media != nil {
		return s.runH265(ctx, c, desc.BaseURL, media, h265)
	}
	return errors.New("stream sin formato H.264/H.265")
}

func (s *Source) runH264(ctx context.Context, c *gortsplib.Client, baseURL *base.URL, media *description.Media, forma *format.H264) error {
	decoder, err := forma.CreateDecoder()
	if err != nil {
		return fmt.Errorf("crear decoder RTP/H264: %w", err)
	}
	if _, err := c.Setup(baseURL, media, 0, 0); err != nil {
		return fmt.Errorf("SETUP RTSP: %w", err)
	}
	sps, pps := forma.SafeParams()
	s.Hub.SetInfo(stream.Info{Codec: "H264", Width: positive(s.Width, 1920), Height: positive(s.Height, 1080), SPS: sps, PPS: pps})

	c.OnPacketRTP(media, forma, func(pkt *rtp.Packet) {
		if s.OnPacket != nil {
			s.OnPacket(len(pkt.Payload))
		}
		s.Hub.PublishRTP(pkt)
		pts, ok := c.PacketPTS(media, pkt)
		if !ok {
			return
		}
		au, decErr := decoder.Decode(pkt)
		if decErr != nil {
			if !errors.Is(decErr, rtph264.ErrNonStartingPacketAndNoPrevious) && !errors.Is(decErr, rtph264.ErrMorePacketsNeeded) {
				return
			}
			return
		}
		key := false
		paramsChanged := false
		for _, nalu := range au {
			if len(nalu) == 0 {
				continue
			}
			switch nalu[0] & 0x1f {
			case 5:
				key = true
			case 7:
				if !bytes.Equal(sps, nalu) {
					sps = append([]byte(nil), nalu...)
					paramsChanged = true
				}
			case 8:
				if !bytes.Equal(pps, nalu) {
					pps = append([]byte(nil), nalu...)
					paramsChanged = true
				}
			}
		}
		if paramsChanged {
			s.Hub.SetInfo(stream.Info{Codec: "H264", Width: positive(s.Width, 1920), Height: positive(s.Height, 1080), SPS: sps, PPS: pps})
		}
		s.Hub.PublishAccessUnit(stream.AccessUnit{PTS: rtpTimestampDuration(pts, forma.ClockRate()), NALUs: au, KeyFrame: key})
	})
	return playUntilDone(ctx, c)
}

func (s *Source) runH265(ctx context.Context, c *gortsplib.Client, baseURL *base.URL, media *description.Media, forma *format.H265) error {
	decoder, err := forma.CreateDecoder()
	if err != nil {
		return fmt.Errorf("crear decoder RTP/H265: %w", err)
	}
	if _, err := c.Setup(baseURL, media, 0, 0); err != nil {
		return fmt.Errorf("SETUP RTSP: %w", err)
	}
	vps, sps, pps := forma.SafeParams()
	s.Hub.SetInfo(stream.Info{Codec: "H265", Width: positive(s.Width, 1920), Height: positive(s.Height, 1080), VPS: vps, SPS: sps, PPS: pps})

	c.OnPacketRTP(media, forma, func(pkt *rtp.Packet) {
		if s.OnPacket != nil {
			s.OnPacket(len(pkt.Payload))
		}
		s.Hub.PublishRTP(pkt)
		pts, ok := c.PacketPTS(media, pkt)
		if !ok {
			return
		}
		au, decErr := decoder.Decode(pkt)
		if decErr != nil {
			if !errors.Is(decErr, rtph265.ErrNonStartingPacketAndNoPrevious) && !errors.Is(decErr, rtph265.ErrMorePacketsNeeded) {
				return
			}
			return
		}
		key := false
		paramsChanged := false
		for _, nalu := range au {
			if len(nalu) < 2 {
				continue
			}
			typ := (nalu[0] >> 1) & 0x3f
			switch typ {
			case 16, 17, 18, 19, 20, 21:
				key = true
			case 32:
				if !bytes.Equal(vps, nalu) {
					vps = append([]byte(nil), nalu...)
					paramsChanged = true
				}
			case 33:
				if !bytes.Equal(sps, nalu) {
					sps = append([]byte(nil), nalu...)
					paramsChanged = true
				}
			case 34:
				if !bytes.Equal(pps, nalu) {
					pps = append([]byte(nil), nalu...)
					paramsChanged = true
				}
			}
		}
		if paramsChanged {
			s.Hub.SetInfo(stream.Info{Codec: "H265", Width: positive(s.Width, 1920), Height: positive(s.Height, 1080), VPS: vps, SPS: sps, PPS: pps})
		}
		s.Hub.PublishAccessUnit(stream.AccessUnit{PTS: rtpTimestampDuration(pts, forma.ClockRate()), NALUs: au, KeyFrame: key})
	})
	return playUntilDone(ctx, c)
}

func playUntilDone(ctx context.Context, c *gortsplib.Client) error {
	if _, err := c.Play(nil); err != nil {
		return fmt.Errorf("PLAY RTSP: %w", err)
	}
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-closed:
		}
	}()
	err := c.Wait()
	close(closed)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func positive(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func rtpTimestampDuration(value int64, clockRate int) time.Duration {
	if clockRate <= 0 {
		return 0
	}
	rate := int64(clockRate)
	seconds := value / rate
	remainder := value % rate
	return time.Duration(seconds)*time.Second + time.Duration(remainder)*time.Second/time.Duration(rate)
}
