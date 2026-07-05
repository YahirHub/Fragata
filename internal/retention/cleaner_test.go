package retention

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fragata/internal/model"
	"fragata/internal/store"
)

func TestCleanupProtectsPartialAndPendingFiles(t *testing.T) {
	root := t.TempDir()
	state, err := store.Open(filepath.Join(root, "state.json"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRetention(model.RetentionPolicy{Enabled: true, Value: 30, Unit: "days"}); err != nil {
		t.Fatal(err)
	}
	recordings := filepath.Join(root, "recordings")
	if err := os.MkdirAll(recordings, 0o750); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -60)
	oldFinal := writeAgedFile(t, recordings, "old.mkv", old)
	newFinal := writeAgedFile(t, recordings, "new.mkv", now.AddDate(0, 0, -2))
	partial := writeAgedFile(t, recordings, "open.mkv.partial", old)
	pending := writeAgedFile(t, recordings, "pending.mkv", old)
	if err := state.SaveUploadJob(model.UploadJob{ID: "job", LocalPath: pending, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	result := (Cleaner{BaseDir: recordings, Store: state}).Cleanup(now)
	if result.Deleted != 1 {
		t.Fatalf("deleted %d files, want 1", result.Deleted)
	}
	if _, err := os.Stat(oldFinal); !os.IsNotExist(err) {
		t.Fatalf("old finalized recording still exists: %v", err)
	}
	for _, protected := range []string{newFinal, partial, pending} {
		if _, err := os.Stat(protected); err != nil {
			t.Fatalf("protected file was removed: %s: %v", protected, err)
		}
	}
}

func writeAgedFile(t *testing.T, dir, name string, modified time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("video"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}
