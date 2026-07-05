package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fragata/internal/stream"
)

func TestRecorderRotatesWithoutWaitingForPreviousFinalize(t *testing.T) {
	hub := stream.NewHub()
	hub.SetInfo(stream.Info{
		Codec: "H264", Width: 1280, Height: 720,
		SPS: []byte{0x67, 0x64, 0x00, 0x1f}, PPS: []byte{0x68, 0xee, 0x3c, 0x80},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan CompletedFile, 4)
	done := make(chan error, 1)
	recorder := Recorder{
		CameraID: "cam-test", BaseDir: t.TempDir(), Hub: hub,
		SegmentDurationProvider: func() time.Duration { return 20 * time.Millisecond },
		OnCompleted:             func(file CompletedFile) { completed <- file },
	}
	go func() { done <- recorder.Run(ctx) }()

	time.Sleep(10 * time.Millisecond)
	generation := hub.BeginSource()
	hub.PublishAccessUnit(stream.AccessUnit{
		Generation: generation, PTS: 0, KeyFrame: true,
		NALUs: [][]byte{{0x65, 0x01, 0x02, 0x03}},
	})
	time.Sleep(30 * time.Millisecond)
	hub.PublishAccessUnit(stream.AccessUnit{
		Generation: generation, PTS: time.Second, KeyFrame: true,
		NALUs: [][]byte{{0x65, 0x04, 0x05, 0x06}},
	})
	hub.EndSource(generation)

	files := make([]CompletedFile, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(files) < 2 {
		select {
		case file := <-completed:
			files = append(files, file)
		case <-deadline:
			t.Fatalf("expected two finalized segments, got %d", len(files))
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recorder did not stop")
	}

	for _, file := range files {
		if filepath.Ext(file.Path) != ".mkv" {
			t.Fatalf("unexpected extension: %s", file.Path)
		}
		info, err := os.Stat(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() < 100 {
			t.Fatalf("segment too small: %s (%d bytes)", file.Path, info.Size())
		}
	}
}
