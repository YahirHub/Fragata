package rtsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCustomTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rtsp-paths.txt")
	content := "# comentario\n/custom\n8554|/secondary\nMarca X|554|/main?channel=1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	templates, err := LoadCustomTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 3 {
		t.Fatalf("got %d templates", len(templates))
	}
	if templates[1].Port != 8554 || templates[2].Name != "Marca X" {
		t.Fatalf("unexpected templates: %#v", templates)
	}
}

func TestExpandCandidatesPreservesPortPriorityAndQuery(t *testing.T) {
	custom := []CandidateTemplate{{Name: "Custom", Path: "/custom?stream=1"}}
	candidates := ExpandCandidates("192.168.10.50", []int{8554, 554}, custom, 200)
	if len(candidates) == 0 {
		t.Fatal("no candidates")
	}
	if candidates[0].Port != 8554 {
		t.Fatalf("first port = %d", candidates[0].Port)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.URL == "rtsp://192.168.10.50:8554/custom?stream=1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom candidate not expanded")
	}
}

func TestExtractCredentials(t *testing.T) {
	user, password, ok := ExtractCredentials("rtsp://camera:p%40ss@192.168.1.5:554/live")
	if !ok || user != "camera" || password != "p@ss" {
		t.Fatalf("got user=%q password=%q ok=%v", user, password, ok)
	}
}
