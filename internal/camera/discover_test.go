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
	if camera.Record {
		t.Fatal("una cámara nueva no debe comenzar a grabar sin autorización explícita")
	}
}

func TestBetterVideoPrefersPixelsBeforeCodec(t *testing.T) {
	if !betterVideo(2304, 1296, "H265", 2, 640, 480, "H264", 1) {
		t.Fatal("la resolución máxima debe tener prioridad sobre H.264")
	}
	if betterVideo(640, 480, "H264", 1, 2304, 1296, "H265", 2) {
		t.Fatal("H.264 no debe desplazar un stream de mayor resolución")
	}
}

func TestBetterVideoPrefersH265OnlyOnResolutionTie(t *testing.T) {
	if !betterVideo(1920, 1080, "H265", 2, 1920, 1080, "H264", 1) {
		t.Fatal("H.265 debe ganar solamente al empatar resolución")
	}
}

func TestPreferredDimensionsUsesStreamSPS(t *testing.T) {
	width, height := preferredDimensions(1920, 1080, 2304, 1296)
	if width != 2304 || height != 1296 {
		t.Fatalf("got %dx%d", width, height)
	}
}

func TestPrepareCameraAcceptsDomainAndPublicIP(t *testing.T) {
	cfg := config.Config{AllowPublicCameras: true}
	camera, _, port, err := prepareCamera(cfg, AddRequest{Host: "camera.example.com:8554"})
	if err != nil {
		t.Fatal(err)
	}
	if camera.Host != "camera.example.com" || port != 8554 {
		t.Fatalf("unexpected camera: %#v port=%d", camera, port)
	}

	camera, _, _, err = prepareCamera(cfg, AddRequest{Host: "203.0.113.25"})
	if err != nil {
		t.Fatal(err)
	}
	if camera.Host != "203.0.113.25" {
		t.Fatalf("unexpected public host: %q", camera.Host)
	}
}
