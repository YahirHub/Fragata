package live

import (
	"bytes"
	"testing"

	"fragata/internal/stream"
)

func TestH264FMTPUsesSPSProfile(t *testing.T) {
	got := h264FMTP([]byte{0x67, 0x64, 0x00, 0x29})
	want := "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=640029"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnnexBAccessUnitPrependsParametersToKeyframe(t *testing.T) {
	info := stream.Info{SPS: []byte{0x67, 0x42, 0xe0, 0x1f}, PPS: []byte{0x68, 0xce, 0x06, 0xe2}}
	unit := stream.AccessUnit{KeyFrame: true, NALUs: [][]byte{{0x65, 0x88, 0x84}}}
	got := annexBAccessUnit(info, unit)
	want := []byte{
		0, 0, 0, 1, 0x67, 0x42, 0xe0, 0x1f,
		0, 0, 0, 1, 0x68, 0xce, 0x06, 0xe2,
		0, 0, 0, 1, 0x65, 0x88, 0x84,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected Annex-B payload: %x", got)
	}
}

func TestAnnexBAccessUnitDoesNotDuplicateParameters(t *testing.T) {
	info := stream.Info{SPS: []byte{0x67, 1}, PPS: []byte{0x68, 2}}
	unit := stream.AccessUnit{KeyFrame: true, NALUs: [][]byte{{0x67, 3}, {0x68, 4}, {0x65, 5}}}
	got := annexBAccessUnit(info, unit)
	if bytes.Count(got, []byte{0, 0, 0, 1, 0x67}) != 1 {
		t.Fatalf("SPS duplicated: %x", got)
	}
	if bytes.Count(got, []byte{0, 0, 0, 1, 0x68}) != 1 {
		t.Fatalf("PPS duplicated: %x", got)
	}
}

func TestNewReservesSeparateVideoAndAudioPeers(t *testing.T) {
	manager := New(nil, 3)
	if manager.maxPeers != 6 {
		t.Fatalf("maxPeers = %d, want 6", manager.maxPeers)
	}
}

func TestWebRTCAudioCapability(t *testing.T) {
	capability, ok := webRTCAudioCapability(stream.AudioInfo{Codec: "PCMU", SampleRate: 8000, Channels: 1})
	if !ok {
		t.Fatal("expected PCMU to be supported")
	}
	if capability.ClockRate != 8000 {
		t.Fatalf("clock rate = %d, want 8000", capability.ClockRate)
	}
	if _, ok := webRTCAudioCapability(stream.AudioInfo{Codec: "AAC", SampleRate: 48000, Channels: 2}); ok {
		t.Fatal("AAC must be converted before WebRTC")
	}
}
