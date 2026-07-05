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
