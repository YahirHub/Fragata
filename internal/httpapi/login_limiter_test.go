package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiterBlocksIPAndUser(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(3, time.Minute, 10*time.Minute)
	limiter.now = func() time.Time { return now }

	for attempt := 0; attempt < 2; attempt++ {
		if allowed, _ := limiter.Allow("192.0.2.10", "admin"); !allowed {
			t.Fatalf("attempt %d should be allowed", attempt+1)
		}
		if retry := limiter.Fail("192.0.2.10", "admin"); retry != 0 {
			t.Fatalf("unexpected early block: %v", retry)
		}
	}
	if retry := limiter.Fail("192.0.2.10", "admin"); retry != 10*time.Minute {
		t.Fatalf("unexpected block duration: %v", retry)
	}
	if allowed, retry := limiter.Allow("192.0.2.10", "other-user"); allowed || retry != 10*time.Minute {
		t.Fatalf("IP should be blocked, allowed=%v retry=%v", allowed, retry)
	}
	if allowed, retry := limiter.Allow("198.51.100.20", "admin"); !allowed || retry != 0 {
		t.Fatalf("another IP must not be able to lock the account globally, allowed=%v retry=%v", allowed, retry)
	}

	now = now.Add(11 * time.Minute)
	if allowed, retry := limiter.Allow("192.0.2.10", "admin"); !allowed || retry != 0 {
		t.Fatalf("block should expire, allowed=%v retry=%v", allowed, retry)
	}
}

func TestLoginLimiterSuccessClearsBuckets(t *testing.T) {
	limiter := newLoginLimiter(3, time.Minute, 10*time.Minute)
	limiter.Fail("192.0.2.20", "admin")
	limiter.Success("192.0.2.20", "admin")
	if len(limiter.buckets) != 0 {
		t.Fatalf("expected empty buckets, got %d", len(limiter.buckets))
	}
}

func TestRemoteIPTrustsForwardedOnlyFromLoopback(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/login", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("X-Forwarded-For", "203.0.113.8, 198.51.100.44")
	if got := remoteIP(request); got != "198.51.100.44" {
		t.Fatalf("got %q", got)
	}

	request.RemoteAddr = "192.0.2.20:54321"
	if got := remoteIP(request); got != "192.0.2.20" {
		t.Fatalf("untrusted proxy header was accepted: %q", got)
	}
}
