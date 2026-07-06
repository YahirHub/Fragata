package store

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"fragata/internal/model"
)

func TestDetectionEventsBetween(t *testing.T) {
	state, err := Open(filepath.Join(t.TempDir(), "state.json"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	for _, event := range []model.DetectionEvent{
		{ID: "before", CameraID: "cam-1", CreatedAt: start.Add(-time.Second)},
		{ID: "second", CameraID: "cam-1", CreatedAt: start.Add(2 * time.Hour)},
		{ID: "first", CameraID: "cam-1", CreatedAt: start.Add(time.Hour)},
		{ID: "other", CameraID: "cam-2", CreatedAt: start.Add(time.Hour)},
	} {
		if err := state.SaveDetectionEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	got := state.DetectionEventsBetween("cam-1", start, start.Add(24*time.Hour), 10)
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "second" {
		t.Fatalf("unexpected events: %#v", got)
	}
}
