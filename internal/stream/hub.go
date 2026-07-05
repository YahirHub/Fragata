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
	PTS           time.Duration
	NALUs         [][]byte
	KeyFrame      bool
	Generation    uint64
	Discontinuity bool
}

type accessUnitSubscription struct {
	channel  chan AccessUnit
	reliable bool
	done     chan struct{}
	once     sync.Once
}

type Hub struct {
	mu         sync.RWMutex
	info       Info
	rtpSubs    map[chan *rtp.Packet]struct{}
	auSubs     map[*accessUnitSubscription]struct{}
	generation uint64
	closed     bool
}

func NewHub() *Hub {
	return &Hub{rtpSubs: make(map[chan *rtp.Packet]struct{}), auSubs: make(map[*accessUnitSubscription]struct{})}
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

// BeginSource identifies a new RTSP session. Recorders use the generation to
// close the previous file before timestamps restart after a reconnect.
func (h *Hub) BeginSource() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0
	}
	h.generation++
	return h.generation
}

// EndSource informs subscribers that the current RTSP session ended. The
// marker contains no video and is never written to a recording.
func (h *Hub) EndSource(generation uint64) {
	if generation == 0 {
		return
	}
	h.PublishAccessUnit(AccessUnit{Generation: generation, Discontinuity: true})
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
	if h.closed {
		h.mu.RUnlock()
		return
	}
	subscriptions := make([]*accessUnitSubscription, 0, len(h.auSubs))
	for subscription := range h.auSubs {
		subscriptions = append(subscriptions, subscription)
	}
	h.mu.RUnlock()

	for _, subscription := range subscriptions {
		copyAU := cloneAccessUnit(au)
		if subscription.reliable {
			select {
			case subscription.channel <- copyAU:
			case <-subscription.done:
			}
			continue
		}
		select {
		case subscription.channel <- copyAU:
		case <-subscription.done:
		default:
		}
	}
}

func cloneAccessUnit(au AccessUnit) AccessUnit {
	out := AccessUnit{
		PTS:           au.PTS,
		KeyFrame:      au.KeyFrame,
		Generation:    au.Generation,
		Discontinuity: au.Discontinuity,
		NALUs:         make([][]byte, len(au.NALUs)),
	}
	for i := range au.NALUs {
		out.NALUs[i] = append([]byte(nil), au.NALUs[i]...)
	}
	return out
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
	return h.subscribeAccessUnits(size, false)
}

func (h *Hub) SubscribeAccessUnitsReliable(size int) (<-chan AccessUnit, func()) {
	return h.subscribeAccessUnits(size, true)
}

func (h *Hub) subscribeAccessUnits(size int, reliable bool) (<-chan AccessUnit, func()) {
	if size < 1 {
		size = 64
	}
	subscription := &accessUnitSubscription{
		channel:  make(chan AccessUnit, size),
		reliable: reliable,
		done:     make(chan struct{}),
	}
	h.mu.Lock()
	if h.closed {
		close(subscription.done)
	} else {
		h.auSubs[subscription] = struct{}{}
	}
	h.mu.Unlock()
	return subscription.channel, func() {
		subscription.once.Do(func() {
			close(subscription.done)
			h.mu.Lock()
			delete(h.auSubs, subscription)
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
	for subscription := range h.auSubs {
		subscription.once.Do(func() { close(subscription.done) })
	}
	h.rtpSubs = nil
	h.auSubs = nil
	h.mu.Unlock()
}
