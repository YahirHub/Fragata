package stream

import (
	"sync"
	"time"

	"github.com/pion/rtp"
)

type Info struct {
	Codec  string
	Width  int
	Height int
	VPS    []byte
	SPS    []byte
	PPS    []byte
}

type AccessUnit struct {
	PTS      time.Duration
	NALUs    [][]byte
	KeyFrame bool
}

type Hub struct {
	mu      sync.RWMutex
	info    Info
	rtpSubs map[chan *rtp.Packet]struct{}
	auSubs  map[chan AccessUnit]struct{}
	closed  bool
}

func NewHub() *Hub {
	return &Hub{rtpSubs: make(map[chan *rtp.Packet]struct{}), auSubs: make(map[chan AccessUnit]struct{})}
}

func (h *Hub) SetInfo(info Info) {
	h.mu.Lock()
	info.VPS = append([]byte(nil), info.VPS...)
	info.SPS = append([]byte(nil), info.SPS...)
	info.PPS = append([]byte(nil), info.PPS...)
	h.info = info
	h.mu.Unlock()
}

func (h *Hub) Info() Info {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := h.info
	out.VPS = append([]byte(nil), out.VPS...)
	out.SPS = append([]byte(nil), out.SPS...)
	out.PPS = append([]byte(nil), out.PPS...)
	return out
}

func (h *Hub) PublishRTP(pkt *rtp.Packet) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return
	}
	for ch := range h.rtpSubs {
		clone := pkt.Clone()
		select {
		case ch <- clone:
		default:
		}
	}
}

func (h *Hub) PublishAccessUnit(au AccessUnit) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return
	}
	for ch := range h.auSubs {
		copyAU := AccessUnit{PTS: au.PTS, KeyFrame: au.KeyFrame, NALUs: make([][]byte, len(au.NALUs))}
		for i := range au.NALUs {
			copyAU.NALUs[i] = append([]byte(nil), au.NALUs[i]...)
		}
		select {
		case ch <- copyAU:
		default:
		}
	}
}

func (h *Hub) SubscribeRTP(size int) (<-chan *rtp.Packet, func()) {
	if size < 1 {
		size = 256
	}
	ch := make(chan *rtp.Packet, size)
	h.mu.Lock()
	if h.closed {
		close(ch)
	} else {
		h.rtpSubs[ch] = struct{}{}
	}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			if _, ok := h.rtpSubs[ch]; ok {
				delete(h.rtpSubs, ch)
				close(ch)
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) SubscribeAccessUnits(size int) (<-chan AccessUnit, func()) {
	if size < 1 {
		size = 64
	}
	ch := make(chan AccessUnit, size)
	h.mu.Lock()
	if h.closed {
		close(ch)
	} else {
		h.auSubs[ch] = struct{}{}
	}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			if _, ok := h.auSubs[ch]; ok {
				delete(h.auSubs, ch)
				close(ch)
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) ViewerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rtpSubs)
}

func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	for ch := range h.rtpSubs {
		close(ch)
	}
	for ch := range h.auSubs {
		close(ch)
	}
	h.rtpSubs = nil
	h.auSubs = nil
	h.mu.Unlock()
}
