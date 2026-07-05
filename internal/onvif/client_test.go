package onvif

import "testing"

func TestNormalizeEndpointHost(t *testing.T) {
	got, err := normalizeEndpointHost("http://203.0.113.4:8899/onvif/media_service", "http://192.168.1.100/onvif/device_service")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://192.168.1.100:8899/onvif/media_service" {
		t.Fatalf("got %q", got)
	}

	got, err = normalizeEndpointHost("/onvif/media_service", "http://[2001:db8::10]:8000/onvif/device_service")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://[2001:db8::10]:8000/onvif/media_service" {
		t.Fatalf("IPv6 got %q", got)
	}
}

func TestNormalizeEndpointHostRejectsOtherSchemes(t *testing.T) {
	if _, err := normalizeEndpointHost("file:///etc/passwd", "http://192.168.1.100/onvif/device_service"); err == nil {
		t.Fatal("expected scheme rejection")
	}
}
