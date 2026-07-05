package model

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	input := "falló rtsp://admin:super-secret@192.168.1.100:554/live, reintento"
	got := RedactSecrets(input)
	if strings.Contains(got, "super-secret") || strings.Contains(got, "admin") {
		t.Fatalf("credentials leaked: %q", got)
	}
	if !strings.Contains(got, "192.168.1.100:554/live") {
		t.Fatalf("host/path removed: %q", got)
	}
	if !strings.HasSuffix(got, ", reintento") {
		t.Fatalf("punctuation damaged: %q", got)
	}
}

func TestUploadJobPublicOmitsLocalSecrets(t *testing.T) {
	job := UploadJob{LocalPath: "/srv/private/video.mkv", SHA256: "secret-hash", LastError: "rtsp://u:p@10.0.0.2/live"}
	public := job.Public()
	if strings.Contains(public.LastError, "u:p") {
		t.Fatalf("credentials leaked: %q", public.LastError)
	}
}
