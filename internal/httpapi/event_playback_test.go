package httpapi

import (
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
