package config

import (
	"net"
	"testing"
)

func TestOptionalAuthentication(t *testing.T) {
	t.Setenv("FRAGATA_DATA_DIR", t.TempDir())
	t.Setenv("FRAGATA_ADMIN_USER", "admin")
	t.Setenv("FRAGATA_ADMIN_PASSWORD", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthEnabled() {
		t.Fatal("la autenticación debe permanecer deshabilitada con credenciales incompletas")
	}

	t.Setenv("FRAGATA_ADMIN_PASSWORD", "secret")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AuthEnabled() {
		t.Fatal("la autenticación debe activarse con usuario y contraseña")
	}
}

func TestViewerLimitValidation(t *testing.T) {
	t.Setenv("FRAGATA_DATA_DIR", t.TempDir())
	t.Setenv("FRAGATA_MAX_VIEWERS", "0")
	if _, err := Load(""); err == nil {
		t.Fatal("se esperaba error por límite de vistas inválido")
	}
}

func TestPrivateIPs(t *testing.T) {
	for _, raw := range []string{"192.168.1.100", "10.0.0.1", "172.16.1.2", "127.0.0.1", "fe80::1"} {
		if !isPrivateOrLocal(net.ParseIP(raw)) {
			t.Fatalf("%s debe ser privada/local", raw)
		}
	}
	if isPrivateOrLocal(net.ParseIP("8.8.8.8")) {
		t.Fatal("8.8.8.8 no debe ser privada")
	}
}

func TestNormalizeCameraHostInputAcceptsDomainsAndPorts(t *testing.T) {
	tests := []struct {
		raw  string
		host string
		port int
	}{
		{raw: "camera.example.com", host: "camera.example.com"},
		{raw: "CAMERA.EXAMPLE.COM:8554", host: "camera.example.com", port: 8554},
		{raw: "203.0.113.20:554", host: "203.0.113.20", port: 554},
		{raw: "[2001:db8::20]:8554", host: "2001:db8::20", port: 8554},
	}
	for _, tt := range tests {
		host, port, err := NormalizeCameraHostInput(tt.raw)
		if err != nil {
			t.Fatalf("%s: %v", tt.raw, err)
		}
		if host != tt.host || port != tt.port {
			t.Fatalf("%s: got %s:%d", tt.raw, host, port)
		}
	}
}

func TestValidateCameraHostAllowsExternalByConfiguration(t *testing.T) {
	cfg := Config{AllowPublicCameras: true}
	for _, host := range []string{"camera.example.com", "8.8.8.8", "2001:4860:4860::8888"} {
		if err := cfg.ValidateCameraHost(host); err != nil {
			t.Fatalf("%s should be accepted: %v", host, err)
		}
	}
	if err := cfg.ValidateCameraHost("0.0.0.0"); err == nil {
		t.Fatal("0.0.0.0 must not be accepted as a camera destination")
	}
}

func TestValidateCameraHostRestrictedMode(t *testing.T) {
	cfg := Config{AllowPublicCameras: false}
	if err := cfg.ValidateCameraHost("192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateCameraHost("8.8.8.8"); err == nil {
		t.Fatal("public IP should be rejected in restricted mode")
	}
}

func TestResolveListenAddressFromHostAndPort(t *testing.T) {
	t.Setenv("FRAGATA_LISTEN", "")
	t.Setenv("FRAGATA_LISTEN_HOST", "0.0.0.0")
	t.Setenv("FRAGATA_LISTEN_PORT", "9090")
	got, err := resolveListenAddress()
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0.0.0:9090" {
		t.Fatalf("got %q", got)
	}
}
