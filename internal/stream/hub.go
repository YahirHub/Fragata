package stream

import (
	"sync"
	"time"

	"github.com/pion/rtp"
)

const (
	maxCachedGOPUnits = 600
	maxCachedGOPBytes = 24 << 20
)

type Info struct {
	Codec  string
	Width  int
	Height int
	VPS    []byte
	SPS    []byte
	PPS    []byte
}

type AudioInfo struct {
	Codec        string
	SampleRate   int
	Channels     int
	CodecPrivate []byte
}

type AccessUnit struct {
	PTS           time.Duration
	NALUs         [][]byte
	KeyFrame      bool
	Generation    uint64
	Discontinuity bool
}

type AudioPacket struct {
	PTS           time.Duration
	Payload       []byte
	RTP           *rtp.Packet
	Generation    uint64
	Discontinuity bool
}

type accessUnitSubscription struct {
	channel  chan AccessUnit
	reliable bool
	done     chan struct{}
	once     sync.Once
}

type audioSubscription struct {
	channel  chan AudioPacket
	reliable bool
	done     chan struct{}
	once     sync.Once
}

type Hub struct {
	mu         sync.RWMutex
	info       Info
	audioInfo  AudioInfo
	rtpSubs    map[chan *rtp.Packet]struct{}
	auSubs     map[*accessUnitSubscription]struct{}
	audioSubs  map[*audioSubscription]struct{}
	generation uint64
	viewers    int
	closed     bool
	cachedGOP  []AccessUnit
	cachedSize int
}

func NewHub() *Hub {
	return &Hub{
		rtpSubs:   make(map[chan *rtp.Packet]struct{}),
		auSubs:    make(map[*accessUnitSubscription]struct{}),
		audioSubs: make(map[*audioSubscription]struct{}),
	}
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

func (h *Hub) SetAudioInfo(info AudioInfo) {
	h.mu.Lock()
	info.CodecPrivate = append([]byte(nil), info.CodecPrivate...)
	h.audioInfo = info
	h.mu.Unlock()
}

func (h *Hub) AudioInfo() AudioInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := h.audioInfo
	out.CodecPrivate = append([]byte(nil), out.CodecPrivate...)
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
	h.audioInfo = AudioInfo{}
	h.clearCachedGOPLocked()
	return h.generation
}

// EndSource informs subscribers that the current RTSP session ended. Markers
// contain no media and are never written to a recording.
func (h *Hub) EndSource(generation uint64) {
	if generation == 0 {
		return
	}
	h.PublishAccessUnit(AccessUnit{Generation: generation, Discontinuity: true})
	h.PublishAudio(AudioPacket{Generation: generation, Discontinuity: true})
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
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.cacheAccessUnitLocked(au)
	subscriptions := make([]*accessUnitSubscription, 0, len(h.auSubs))
	for subscription := range h.auSubs {
		subscriptions = append(subscriptions, subscription)
	}
	h.mu.Unlock()

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

func (h *Hub) PublishAudio(packet AudioPacket) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return
	}
	subscriptions := make([]*audioSubscription, 0, len(h.audioSubs))
	for subscription := range h.audioSubs {
		subscriptions = append(subscriptions, subscription)
	}
	h.mu.RUnlock()
	for _, subscription := range subscriptions {
		copyPacket := cloneAudioPacket(packet)
		if subscription.reliable {
			select {
			case subscription.channel <- copyPacket:
			case <-subscription.done:
			}
			continue
		}
		select {
		case subscription.channel <- copyPacket:
		case <-subscription.done:
		default:
		}
	}
}

func (h *Hub) cacheAccessUnitLocked(au AccessUnit) {
	if au.Discontinuity {
		h.clearCachedGOPLocked()
		return
	}
	if len(au.NALUs) == 0 {
		return
	}
	if au.KeyFrame {
		h.clearCachedGOPLocked()
		h.cachedGOP = append(h.cachedGOP, cloneAccessUnit(au))
		h.cachedSize = accessUnitSize(au)
		return
	}
	if len(h.cachedGOP) == 0 {
		return
	}
	if h.cachedGOP[0].Generation != 0 && au.Generation != 0 && h.cachedGOP[0].Generation != au.Generation {
		h.clearCachedGOPLocked()
		return
	}
	nextSize := h.cachedSize + accessUnitSize(au)
	if len(h.cachedGOP) >= maxCachedGOPUnits || nextSize > maxCachedGOPBytes {
		// ponytail: discard an oversized GOP instead of keeping an unbounded
		// buffer; live viewers will wait for the next keyframe.
		h.clearCachedGOPLocked()
		return
	}
	h.cachedGOP = append(h.cachedGOP, cloneAccessUnit(au))
	h.cachedSize = nextSize
}

func (h *Hub) clearCachedGOPLocked() {
	h.cachedGOP = nil
	h.cachedSize = 0
}

func accessUnitSize(au AccessUnit) int {
	total := 0
	for _, nalu := range au.NALUs {
		total += len(nalu)
	}
	return total
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

func cloneAudioPacket(packet AudioPacket) AudioPacket {
	out := AudioPacket{
		PTS:           packet.PTS,
		Payload:       append([]byte(nil), packet.Payload...),
		Generation:    packet.Generation,
		Discontinuity: packet.Discontinuity,
	}
	if packet.RTP != nil {
		out.RTP = packet.RTP.Clone()
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

	h.mu.Lock()
	cached := []AccessUnit(nil)
	if !reliable && len(h.cachedGOP) > 0 {
		cached = make([]AccessUnit, len(h.cachedGOP))
		for i := range h.cachedGOP {
			cached[i] = cloneAccessUnit(h.cachedGOP[i])
		}
		if minimum := len(cached) + 32; size < minimum {
			size = minimum
		}
	}
	subscription := &accessUnitSubscription{channel: make(chan AccessUnit, size), reliable: reliable, done: make(chan struct{})}
	if h.closed {
		close(subscription.done)
	} else {
		for _, au := range cached {
			subscription.channel <- au
		}
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

func (h *Hub) SubscribeAudio(size int) (<-chan AudioPacket, func()) {
	return h.subscribeAudio(size, false)
}

func (h *Hub) SubscribeAudioReliable(size int) (<-chan AudioPacket, func()) {
	return h.subscribeAudio(size, true)
}

func (h *Hub) subscribeAudio(size int, reliable bool) (<-chan AudioPacket, func()) {
	if size < 1 {
		size = 256
	}
	subscription := &audioSubscription{channel: make(chan AudioPacket, size), reliable: reliable, done: make(chan struct{})}
	h.mu.Lock()
	if h.closed {
		close(subscription.done)
	} else {
		h.audioSubs[subscription] = struct{}{}
	}
	h.mu.Unlock()
	return subscription.channel, func() {
		subscription.once.Do(func() {
			close(subscription.done)
			h.mu.Lock()
			delete(h.audioSubs, subscription)
			h.mu.Unlock()
		})
	}
}

func (h *Hub) AcquireViewer() func() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return func() {}
	}
	h.viewers++
	h.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			if h.viewers > 0 {
				h.viewers--
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) ViewerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rtpSubs) + h.viewers
}

func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.clearCachedGOPLocked()
	for ch := range h.rtpSubs {
		close(ch)
	}
	for subscription := range h.auSubs {
		subscription.once.Do(func() { close(subscription.done) })
	}
	for subscription := range h.audioSubs {
		subscription.once.Do(func() { close(subscription.done) })
	}
	h.rtpSubs = nil
	h.auSubs = nil
	h.audioSubs = nil
	h.mu.Unlock()
}
