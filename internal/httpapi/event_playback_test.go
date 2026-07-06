package httpapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeRecordingPath(t *testing.T) {
	root := t.TempDir()
	path, ok := safeRecordingPath(root, "entrada/2026/07/05/10-00-00.000.mkv")
	if !ok {
		t.Fatal("se esperaba una ruta válida")
	}
	if filepath.Dir(path) == root {
		t.Fatal("la ruta debe conservar sus subdirectorios")
	}
	for _, candidate := range []string{"../secret", "/etc/passwd", "entrada/../../secret"} {
		if _, ok := safeRecordingPath(root, candidate); ok {
			t.Fatalf("se aceptó una ruta insegura: %s", candidate)
		}
	}
}

func TestRecordingStartFromName(t *testing.T) {
	day := time.Date(2026, 7, 5, 0, 0, 0, 0, time.Local)
	started, ok := recordingStartFromName(day, "14-25-30.125-01.recovered.mkv")
	if !ok {
		t.Fatal("no se pudo interpretar el nombre del segmento")
	}
	if started.Hour() != 14 || started.Minute() != 25 || started.Second() != 30 || started.Nanosecond() != 125_000_000 {
		t.Fatalf("hora inesperada: %v", started)
	}
}

func TestSafeRecordingPathRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mkv"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok := safeRecordingPath(root, "linked/secret.mkv"); ok {
		t.Fatal("a recording path through a symlink must be rejected")
	}
}

func TestRecordingIDRoundTrip(t *testing.T) {
	relative := "entrada/2026/07/05/14-25-30.125.mkv"
	id := encodeRecordingID(relative)
	decoded, err := decodeRecordingID(id)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != relative {
		t.Fatalf("got %q", decoded)
	}
}

func TestParsePlaybackStart(t *testing.T) {
	start, err := parsePlaybackStart("12.5", time.Minute)
	if err != nil || start != 12500*time.Millisecond {
		t.Fatalf("unexpected start: %v, %v", start, err)
	}
	if _, err := parsePlaybackStart("60", time.Minute); err == nil {
		t.Fatal("start at duration should be rejected")
	}
}

func TestTrimRecordingDaysKeepsNewest(t *testing.T) {
	days := map[string]recordingDay{
		"2026-07-01": {Date: "2026-07-01"},
		"2026-07-02": {Date: "2026-07-02"},
		"2026-07-03": {Date: "2026-07-03"},
	}
	trimRecordingDays(days, 2)
	if len(days) != 2 {
		t.Fatalf("got %d days", len(days))
	}
	if _, exists := days["2026-07-01"]; exists {
		t.Fatal("oldest day should be removed")
	}
}
