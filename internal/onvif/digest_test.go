package onvif

import (
	"strings"
	"testing"
)

func TestDigestAuthorization(t *testing.T) {
	ch, err := parseDigestChallenge(`Digest realm="cam", nonce="abc", qop="auth", opaque="x"`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := digestAuthorization(ch, "POST", "/onvif/device_service", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`username="admin"`, `realm="cam"`, `nonce="abc"`, `qop=auth`, `uri="/onvif/device_service"`} {
		if !strings.Contains(a, part) {
			t.Fatalf("falta %s en %s", part, a)
		}
	}
}
