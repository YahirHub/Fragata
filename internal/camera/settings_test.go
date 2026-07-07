package camera

import "testing"

func TestNormalizeFolderName(t *testing.T) {
	got, err := normalizeFolderName(" Oficina Principal / Entrada ", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "oficina-principal-entrada" {
		t.Fatalf("unexpected folder: %q", got)
	}
}

func TestNormalizeFolderNameUsesFallback(t *testing.T) {
	got, err := normalizeFolderName("", "Cámara Pasillo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "camara-pasillo" {
		t.Fatalf("unexpected fallback folder: %q", got)
	}
}

func TestValidateSegmentDuration(t *testing.T) {
	if err := validateSegmentDuration(3600); err != nil {
		t.Fatal(err)
	}
	if err := validateSegmentDuration(90); err == nil {
		t.Fatal("expected minute precision error")
	}
}
