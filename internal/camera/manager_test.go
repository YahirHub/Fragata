package camera

import (
	"testing"

	"fragata/internal/stream"
)

func TestBrowserAudioReady(t *testing.T) {
	for _, codec := range []string{"PCMA", "PCMU", "OPUS"} {
		if !browserAudioReady(stream.AudioInfo{Codec: codec}) {
			t.Fatalf("expected %s to be browser-ready", codec)
		}
	}
	if browserAudioReady(stream.AudioInfo{Codec: "AAC"}) {
		t.Fatal("AAC must not be browser-ready without conversion")
	}
}
