package stream

import "testing"

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
