package rtsp

import (
	"strings"
	"testing"
	"time"
)

func TestRTPTimestampDuration(t *testing.T) {
	for _, test := range []struct {
		value int64
		want  time.Duration
	}{
		{0, 0},
		{90_000, time.Second},
		{135_000, 1500 * time.Millisecond},
		{-45_000, -500 * time.Millisecond},
	} {
		if got := rtpTimestampDuration(test.value, 90_000); got != test.want {
			t.Fatalf("timestamp %d: got %s, want %s", test.value, got, test.want)
		}
	}
}

func TestNormalizeHostForcesConfiguredCamera(t *testing.T) {
	got := NormalizeHost("rtsp://203.0.113.8:8554/live", "192.168.1.100")
	if got != "rtsp://192.168.1.100:8554/live" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeHost("rtsp://localhost/live", "2001:db8::10"); got != "rtsp://[2001:db8::10]:554/live" {
		t.Fatalf("IPv6 got %q", got)
	}
}

func TestCommonCandidatesAreValidForIPv6(t *testing.T) {
	candidates := CommonCandidates("2001:db8::10")
	if len(candidates) == 0 || !strings.HasPrefix(candidates[0], "rtsp://[2001:db8::10]:554/") {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}
