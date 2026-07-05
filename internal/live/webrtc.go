package live

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"fragata/internal/stream"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type Manager struct {
	configuration webrtc.Configuration
	mu            sync.Mutex
	peers         map[*webrtc.PeerConnection]context.CancelFunc
	maxPeers      int
}

func New(stunServers []string, maxPeers int) *Manager {
	servers := make([]webrtc.ICEServer, 0, 1)
	if len(stunServers) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: stunServers})
	}
	if maxPeers < 1 {
		maxPeers = 1
	}
	// Cada visor puede usar una sesión para video y otra opcional para audio.
	maxPeers *= 2
	return &Manager{configuration: webrtc.Configuration{ICEServers: servers}, peers: make(map[*webrtc.PeerConnection]context.CancelFunc), maxPeers: maxPeers}
}

// OfferVideo creates an isolated video-only WebRTC session. Keeping audio in a
// separate peer prevents a camera audio negotiation problem from blocking or
// restarting a healthy video session.
func (m *Manager) OfferVideo(ctx context.Context, hub *stream.Hub, offerSDP, mode string) (string, error) {
	if hub == nil {
		return "", errors.New("cámara no disponible")
	}
	info := hub.Info()
	if info.Codec == "" {
		return "", errors.New("la cámara todavía no ha iniciado el stream")
	}
	if info.Codec != "H264" {
		return "", errors.New("la vista web requiere video H.264")
	}

	pc, peerCtx, connected, cleanup, err := m.newPeer()
	if err != nil {
		return "", err
	}

	capability := webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: h264FMTP(info.SPS),
	}
	_ = mode // Se conserva para diagnóstico; todo video se normaliza a access units.
	track, err := webrtc.NewTrackLocalStaticSample(capability, "video", "fragata-video")
	if err != nil {
		cleanup()
		return "", err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		cleanup()
		return "", err
	}
	drainRTCP(sender)

	go func() {
		select {
		case <-peerCtx.Done():
			return
		case <-connected:
		}
		releaseViewer := hub.AcquireViewer()
		units, unsubscribe := hub.SubscribeAccessUnits(256)
		defer unsubscribe()
		defer releaseViewer()
		relayAccessUnits(peerCtx, hub, track, units)
	}()

	return completeOffer(ctx, pc, cleanup, offerSDP)
}

// OfferAudio creates an audio-only WebRTC session. Failure here must never
// affect the video peer; callers can retry audio silently in the background.
func (m *Manager) OfferAudio(ctx context.Context, hub *stream.Hub, offerSDP string) (string, error) {
	if hub == nil {
		return "", errors.New("cámara no disponible")
	}
	audioInfo := hub.AudioInfo()
	capability, ok := webRTCAudioCapability(audioInfo)
	if !ok {
		return "", errors.New("el audio todavía no está disponible para el navegador")
	}

	pc, peerCtx, connected, cleanup, err := m.newPeer()
	if err != nil {
		return "", err
	}
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "audio", "fragata-audio")
	if err != nil {
		cleanup()
		return "", err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		cleanup()
		return "", err
	}
	drainRTCP(sender)

	go func() {
		select {
		case <-peerCtx.Done():
			return
		case <-connected:
		}
		packets, unsubscribe := hub.SubscribeAudio(512)
		defer unsubscribe()
		for {
			select {
			case <-peerCtx.Done():
				return
			case packet, open := <-packets:
				if !open {
					return
				}
				if packet.Discontinuity || packet.RTP == nil {
					continue
				}
				if err := track.WriteRTP(packet.RTP); err != nil {
					return
				}
			}
		}
	}()

	return completeOffer(ctx, pc, cleanup, offerSDP)
}

func (m *Manager) newPeer() (*webrtc.PeerConnection, context.Context, <-chan struct{}, func(), error) {
	m.mu.Lock()
	if len(m.peers) >= m.maxPeers {
		m.mu.Unlock()
		return nil, nil, nil, nil, errors.New("se alcanzó el límite de vistas en vivo")
	}
	m.mu.Unlock()

	pc, err := webrtc.NewPeerConnection(m.configuration)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	peerCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	if len(m.peers) >= m.maxPeers {
		m.mu.Unlock()
		cancel()
		_ = pc.Close()
		return nil, nil, nil, nil, errors.New("se alcanzó el límite de vistas en vivo")
	}
	m.peers[pc] = cancel
	m.mu.Unlock()

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			m.mu.Lock()
			delete(m.peers, pc)
			m.mu.Unlock()
			_ = pc.Close()
		})
	}
	connected := make(chan struct{})
	var connectedOnce sync.Once
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			connectedOnce.Do(func() { close(connected) })
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			cleanup()
		}
	})
	return pc, peerCtx, connected, cleanup, nil
}

func completeOffer(ctx context.Context, pc *webrtc.PeerConnection, cleanup func(), offerSDP string) (string, error) {
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		cleanup()
		return "", fmt.Errorf("oferta WebRTC inválida: %w", err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		cleanup()
		return "", err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		cleanup()
		return "", err
	}
	select {
	case <-ctx.Done():
		cleanup()
		return "", ctx.Err()
	case <-gatherComplete:
	case <-time.After(10 * time.Second):
		cleanup()
		return "", errors.New("timeout reuniendo candidatos WebRTC")
	}
	local := pc.LocalDescription()
	if local == nil {
		cleanup()
		return "", errors.New("respuesta WebRTC vacía")
	}
	return local.SDP, nil
}

func relayAccessUnits(ctx context.Context, hub *stream.Hub, track *webrtc.TrackLocalStaticSample, units <-chan stream.AccessUnit) {
	const defaultFrameDuration = time.Second / 30
	var (
		started bool
		havePTS bool
		lastPTS time.Duration
	)
	for {
		select {
		case <-ctx.Done():
			return
		case unit, ok := <-units:
			if !ok {
				return
			}
			if unit.Discontinuity {
				started = false
				havePTS = false
				continue
			}
			if len(unit.NALUs) == 0 {
				continue
			}
			if !started {
				if !unit.KeyFrame {
					continue
				}
				started = true
			}
			duration := defaultFrameDuration
			if havePTS && unit.PTS > lastPTS {
				duration = unit.PTS - lastPTS
				if duration < time.Millisecond || duration > time.Second {
					duration = defaultFrameDuration
				}
			}
			lastPTS = unit.PTS
			havePTS = true
			payload := annexBAccessUnit(hub.Info(), unit)
			if len(payload) == 0 {
				continue
			}
			if err := track.WriteSample(media.Sample{Data: payload, Duration: duration}); err != nil {
				return
			}
		}
	}
}

func annexBAccessUnit(info stream.Info, unit stream.AccessUnit) []byte {
	nalus := make([][]byte, 0, len(unit.NALUs)+2)
	if unit.KeyFrame {
		if len(info.SPS) > 0 && !containsH264NALU(unit.NALUs, 7) {
			nalus = append(nalus, info.SPS)
		}
		if len(info.PPS) > 0 && !containsH264NALU(unit.NALUs, 8) {
			nalus = append(nalus, info.PPS)
		}
	}
	nalus = append(nalus, unit.NALUs...)
	total := 0
	for _, nalu := range nalus {
		if len(nalu) > 0 {
			total += 4 + len(nalu)
		}
	}
	out := make([]byte, 0, total)
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, nalu...)
	}
	return out
}

func containsH264NALU(nalus [][]byte, wanted byte) bool {
	for _, nalu := range nalus {
		if len(nalu) > 0 && nalu[0]&0x1f == wanted {
			return true
		}
	}
	return false
}

func webRTCAudioCapability(info stream.AudioInfo) (webrtc.RTPCodecCapability, bool) {
	switch info.Codec {
	case "PCMA":
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA, ClockRate: 8000}, true
	case "PCMU":
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000}, true
	case "OPUS":
		channels := uint16(info.Channels)
		if channels < 1 || channels > 2 {
			channels = 2
		}
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: channels, SDPFmtpLine: "minptime=10;useinbandfec=1"}, true
	default:
		return webrtc.RTPCodecCapability{}, false
	}
}

func h264FMTP(sps []byte) string {
	profileLevelID := "42e01f"
	if len(sps) >= 4 && sps[0]&0x1f == 7 {
		profileLevelID = hex.EncodeToString(sps[1:4])
	}
	return "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + profileLevelID
}

func drainRTCP(sender *webrtc.RTPSender) {
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
}

func (m *Manager) Close() {
	m.mu.Lock()
	peers := make([]*webrtc.PeerConnection, 0, len(m.peers))
	for pc, cancel := range m.peers {
		cancel()
		peers = append(peers, pc)
	}
	m.peers = make(map[*webrtc.PeerConnection]context.CancelFunc)
	m.mu.Unlock()
	for _, pc := range peers {
		_ = pc.Close()
	}
}
