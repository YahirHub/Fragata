package upload

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/store"
)

func TestCompleteKeepsQueueWhenLocalCleanupFails(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := model.UploadJob{ID: "job-1", LocalPath: local, CreatedAt: time.Now(), NextAttempt: time.Now()}
	if err := state.SaveUploadJob(job); err != nil {
		t.Fatal(err)
	}
	uploader := New(config.SFTPConfig{}, state, nil)
	if err := uploader.complete(job, true); err == nil {
		t.Fatal("expected local cleanup failure")
	}
	if len(state.UploadJobs()) != 1 {
		t.Fatal("queue entry must remain until local cleanup succeeds")
	}
}
