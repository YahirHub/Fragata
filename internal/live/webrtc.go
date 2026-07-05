package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"fragata/internal/stream"
	"github.com/pion/webrtc/v4"
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
	return &Manager{configuration: webrtc.Configuration{ICEServers: servers}, peers: make(map[*webrtc.PeerConnection]context.CancelFunc), maxPeers: maxPeers}
}

func (m *Manager) Offer(ctx context.Context, hub *stream.Hub, offerSDP string) (string, error) {
	if hub == nil {
		return "", errors.New("cámara no disponible")
	}
	info := hub.Info()
	if info.Codec == "" {
		return "", errors.New("la cámara todavía no ha iniciado el stream")
	}
	if info.Codec != "H264" {
		return "", errors.New("la vista web del MVP requiere un perfil H.264; la grabación H.265 sí está permitida")
	}
	m.mu.Lock()
	if len(m.peers) >= m.maxPeers {
		m.mu.Unlock()
		return "", errors.New("se alcanzó el límite de vistas en vivo")
	}
	m.mu.Unlock()
	pc, err := webrtc.NewPeerConnection(m.configuration)
	if err != nil {
		return "", err
	}
	peerCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	if len(m.peers) >= m.maxPeers {
		m.mu.Unlock()
		cancel()
		_ = pc.Close()
		return "", errors.New("se alcanzó el límite de vistas en vivo")
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

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		"video", "fragata",
	)
	if err != nil {
		cleanup()
		return "", err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		cleanup()
		return "", err
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()

	packets, unsubscribe := hub.SubscribeRTP(512)
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-peerCtx.Done():
				return
			case pkt, ok := <-packets:
				if !ok {
					return
				}
				if err := track.WriteRTP(pkt); err != nil {
					return
				}
			}
		}
	}()
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			cleanup()
		}
	})
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
