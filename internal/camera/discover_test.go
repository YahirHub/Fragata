package camera

import (
	"testing"

	"fragata/internal/config"
)

func TestPrepareCameraExtractsCredentialsFromManualURL(t *testing.T) {
	cfg := config.Config{}
	camera, rawURL, port, err := prepareCamera(cfg, AddRequest{
		RTSPURL: "rtsp://user:p%40ss@192.168.10.50:8554/cam/realmonitor?channel=1&subtype=0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rawURL == "" || camera.Host != "192.168.10.50" || port != 8554 {
		t.Fatalf("unexpected camera: %#v port=%d", camera, port)
	}
	if camera.Username != "user" || camera.Password != "p@ss" {
		t.Fatalf("credentials not extracted: user=%q password=%q", camera.Username, camera.Password)
	}
}

func TestPrepareCameraAllowsExplicitCredentials(t *testing.T) {
	cfg := config.Config{}
	camera, _, _, err := prepareCamera(cfg, AddRequest{
		Host: "192.168.10.50", Username: "admin", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if camera.Username != "admin" || camera.Password != "secret" {
		t.Fatalf("unexpected credentials: %#v", camera)
	}
}
