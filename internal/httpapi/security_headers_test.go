package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersOmitsCOOPForUntrustedHTTPOrigin(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), false)

	request := httptest.NewRequest(http.MethodGet, "http://203.0.113.10:8080/", nil)
	request.Host = "203.0.113.10:8080"
	request.RemoteAddr = "198.51.100.20:45678"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if value := response.Header().Get("Cross-Origin-Opener-Policy"); value != "" {
		t.Fatalf("COOP must be omitted for an untrusted HTTP origin, got %q", value)
	}
	if value := response.Header().Get("Strict-Transport-Security"); value != "" {
		t.Fatalf("HSTS must be omitted for plain HTTP, got %q", value)
	}
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' https://cdn.jsdelivr.net") {
		t.Fatalf("CSP must allow jsDelivr source maps, got %q", csp)
	}
}

func TestSecurityHeadersAllowsCOOPForLocalhost(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), false)

	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:45678"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if value := response.Header().Get("Cross-Origin-Opener-Policy"); value != "same-origin" {
		t.Fatalf("COOP must be enabled for localhost, got %q", value)
	}
	if value := response.Header().Get("Strict-Transport-Security"); value != "" {
		t.Fatalf("HSTS must not be sent for HTTP localhost, got %q", value)
	}
}

func TestSecurityHeadersTrustsHTTPSFromLoopbackProxy(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), false)

	request := httptest.NewRequest(http.MethodGet, "http://fragata.internal/", nil)
	request.Host = "fragata.example.com"
	request.RemoteAddr = "127.0.0.1:45678"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if value := response.Header().Get("Cross-Origin-Opener-Policy"); value != "same-origin" {
		t.Fatalf("COOP must be enabled behind a trusted HTTPS proxy, got %q", value)
	}
	if value := response.Header().Get("Strict-Transport-Security"); value == "" {
		t.Fatal("HSTS must be enabled behind a trusted HTTPS proxy")
	}
}

func TestSecurityHeadersIgnoresSpoofedForwardedProtoFromRemotePeer(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), false)

	request := httptest.NewRequest(http.MethodGet, "http://203.0.113.10/", nil)
	request.Host = "203.0.113.10"
	request.RemoteAddr = "198.51.100.20:45678"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if value := response.Header().Get("Cross-Origin-Opener-Policy"); value != "" {
		t.Fatalf("remote clients must not spoof secure context, got COOP %q", value)
	}
	if value := response.Header().Get("Strict-Transport-Security"); value != "" {
		t.Fatalf("remote clients must not spoof HSTS, got %q", value)
	}
}
