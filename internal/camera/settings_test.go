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

func TestValidateSnapshotURLRequiresCameraHost(t *testing.T) {
	got, err := validateSnapshotURL("http://admin:secret@192.168.1.20/cgi-bin/snapshot.cgi?channel=1", "192.168.1.20")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://192.168.1.20/cgi-bin/snapshot.cgi?channel=1" {
		t.Fatalf("unexpected sanitized snapshot URL: %q", got)
	}
	if _, err := validateSnapshotURL("http://192.168.1.99/snapshot.jpg", "192.168.1.20"); err == nil {
		t.Fatal("expected foreign host rejection")
	}
}

func TestNormalizeSnapshotHostUsesConfiguredDomain(t *testing.T) {
	got := normalizeSnapshotHost("http://192.168.1.20:8080/snapshot.jpg", "camera.example.com")
	if got != "http://camera.example.com:8080/snapshot.jpg" {
		t.Fatalf("got %q", got)
	}
	if _, err := validateSnapshotURL(got, "camera.example.com"); err != nil {
		t.Fatal(err)
	}
}
