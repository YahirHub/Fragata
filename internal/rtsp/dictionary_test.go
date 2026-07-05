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

func TestPortFailureSummaryTimeout(t *testing.T) {
	message := PortFailureSummary("192.168.10.234", []PortCheck{
		{Port: 554, State: "timeout"},
		{Port: 80, State: "timeout"},
	})
	if message == "" {
		t.Fatal("expected timeout explanation")
	}
}

func TestPortFailureSummaryRefused(t *testing.T) {
	message := PortFailureSummary("192.168.10.234", []PortCheck{{Port: 554, State: "refused"}})
	if message == "" {
		t.Fatal("expected refused explanation")
	}
}

func TestBetterDictionaryProbePrefersHigherResolutionEvenWhenH265(t *testing.T) {
	current := &dictionaryProbe{
		probe:     ProbeResult{URL: "rtsp://camera/sub", Codec: "H264", Width: 640, Height: 480},
		candidate: Candidate{Name: "Secundario", URL: "rtsp://camera/sub"},
		index:     1,
	}
	candidate := &dictionaryProbe{
		probe:     ProbeResult{URL: "rtsp://camera/main", Codec: "H265", Width: 2304, Height: 1296},
		candidate: Candidate{Name: "Principal", URL: "rtsp://camera/main"},
		index:     2,
	}
	if !betterDictionaryProbe(candidate, current) {
		t.Fatal("el stream H.265 de mayor resolución debe ganar")
	}
}

func TestBetterDictionaryProbeUsesMainRouteWhenDimensionsUnknown(t *testing.T) {
	secondary := &dictionaryProbe{
		probe:     ProbeResult{URL: "rtsp://camera/cam/realmonitor?channel=1&subtype=1", Codec: "H264"},
		candidate: Candidate{Name: "Dahua/Imou secundario", URL: "rtsp://camera/cam/realmonitor?channel=1&subtype=1"},
		index:     1,
	}
	main := &dictionaryProbe{
		probe:     ProbeResult{URL: "rtsp://camera/cam/realmonitor?channel=1&subtype=0", Codec: "H265"},
		candidate: Candidate{Name: "Dahua/Imou principal", URL: "rtsp://camera/cam/realmonitor?channel=1&subtype=0"},
		index:     2,
	}
	if !betterDictionaryProbe(main, secondary) {
		t.Fatal("la ruta principal debe ganar cuando no hay dimensiones")
	}
}
