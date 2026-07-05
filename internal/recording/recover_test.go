package recording

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverPartials(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "clip.mkv.partial")
	if err := os.WriteFile(partial, []byte("matroska"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverPartials(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || filepath.Base(recovered[0]) != "clip.mkv" {
		t.Fatalf("recuperación inesperada: %#v", recovered)
	}
	if _, err := os.Stat(recovered[0]); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRemovesEmptyPartial(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "empty.mkv.partial")
	if err := os.WriteFile(partial, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPartials(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("el parcial vacío debe eliminarse: %v", err)
	}
}
