package stream

import (
	"testing"
	"time"
)

func TestAcquireViewerTracksAndReleasesOnce(t *testing.T) {
	hub := NewHub()
	release := hub.AcquireViewer()
	if got := hub.ViewerCount(); got != 1 {
		t.Fatalf("got %d viewers, want 1", got)
	}
	release()
	release()
	if got := hub.ViewerCount(); got != 0 {
		t.Fatalf("got %d viewers after release, want 0", got)
	}
}

func TestLiveSubscriptionStartsWithCachedGOP(t *testing.T) {
	hub := NewHub()
	hub.PublishAccessUnit(AccessUnit{PTS: 0, KeyFrame: true, Generation: 1, NALUs: [][]byte{{0x65, 0x01}}})
	hub.PublishAccessUnit(AccessUnit{PTS: time.Second / 30, Generation: 1, NALUs: [][]byte{{0x41, 0x02}}})

	units, unsubscribe := hub.SubscribeAccessUnits(1)
	defer unsubscribe()

	first := <-units
	second := <-units
	if !first.KeyFrame || first.NALUs[0][1] != 0x01 {
		t.Fatalf("first cached unit is not the expected keyframe: %#v", first)
	}
	if second.KeyFrame || second.NALUs[0][1] != 0x02 {
		t.Fatalf("second cached unit is not the expected delta frame: %#v", second)
	}
}

func TestReliableSubscriptionDoesNotReplayCachedGOP(t *testing.T) {
	hub := NewHub()
	hub.PublishAccessUnit(AccessUnit{KeyFrame: true, Generation: 1, NALUs: [][]byte{{0x65}}})
	units, unsubscribe := hub.SubscribeAccessUnitsReliable(1)
	defer unsubscribe()

	select {
	case unit := <-units:
		t.Fatalf("reliable subscriber unexpectedly received cached unit: %#v", unit)
	default:
	}
}

func TestDiscontinuityClearsCachedGOP(t *testing.T) {
	hub := NewHub()
	hub.PublishAccessUnit(AccessUnit{KeyFrame: true, Generation: 1, NALUs: [][]byte{{0x65}}})
	hub.EndSource(1)
	units, unsubscribe := hub.SubscribeAccessUnits(1)
	defer unsubscribe()

	select {
	case unit := <-units:
		t.Fatalf("subscriber unexpectedly received stale unit after discontinuity: %#v", unit)
	default:
	}
}

func TestBeginSourceClearsStaleAudioInfo(t *testing.T) {
	hub := NewHub()
	hub.SetAudioInfo(AudioInfo{Codec: "PCMA", SampleRate: 8000, Channels: 1})
	hub.BeginSource()
	if got := hub.AudioInfo(); got.Codec != "" {
		t.Fatalf("stale audio info was retained: %#v", got)
	}
}
