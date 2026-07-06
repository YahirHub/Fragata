package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxTrackedLoginBuckets = 10_000

type loginBucket struct {
	failures     []time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

type loginLimiter struct {
	mu            sync.Mutex
	buckets       map[string]*loginBucket
	maxAttempts   int
	window        time.Duration
	blockDuration time.Duration
	now           func() time.Time
}

func newLoginLimiter(maxAttempts int, window, blockDuration time.Duration) *loginLimiter {
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	if blockDuration <= 0 {
		blockDuration = 10 * time.Minute
	}
	return &loginLimiter{
		buckets:       make(map[string]*loginBucket),
		maxAttempts:   maxAttempts,
		window:        window,
		blockDuration: blockDuration,
		now:           time.Now,
	}
}

func (l *loginLimiter) Allow(ip, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.cleanupLocked(now)
	var retryAfter time.Duration
	for _, key := range loginKeys(ip, username) {
		bucket := l.bucketLocked(key, now)
		l.pruneFailuresLocked(bucket, now)
		if bucket.blockedUntil.After(now) {
			if wait := bucket.blockedUntil.Sub(now); wait > retryAfter {
				retryAfter = wait
			}
		}
	}
	return retryAfter <= 0, retryAfter
}

func (l *loginLimiter) Fail(ip, username string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var retryAfter time.Duration
	for _, key := range loginKeys(ip, username) {
		bucket := l.bucketLocked(key, now)
		l.pruneFailuresLocked(bucket, now)
		bucket.failures = append(bucket.failures, now)
		bucket.lastSeen = now
		if len(bucket.failures) >= l.maxAttempts {
			bucket.blockedUntil = now.Add(l.blockDuration)
			if l.blockDuration > retryAfter {
				retryAfter = l.blockDuration
			}
		}
	}
	l.cleanupLocked(now)
	return retryAfter
}

func (l *loginLimiter) Success(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range loginKeys(ip, username) {
		delete(l.buckets, key)
	}
}

func (l *loginLimiter) bucketLocked(key string, now time.Time) *loginBucket {
	bucket := l.buckets[key]
	if bucket == nil {
		bucket = &loginBucket{}
		l.buckets[key] = bucket
	}
	bucket.lastSeen = now
	return bucket
}

func (l *loginLimiter) pruneFailuresLocked(bucket *loginBucket, now time.Time) {
	cutoff := now.Add(-l.window)
	items := bucket.failures[:0]
	for _, at := range bucket.failures {
		if at.After(cutoff) {
			items = append(items, at)
		}
	}
	bucket.failures = items
	if !bucket.blockedUntil.After(now) {
		bucket.blockedUntil = time.Time{}
	}
}

func (l *loginLimiter) cleanupLocked(now time.Time) {
	staleBefore := now.Add(-(l.window + l.blockDuration + time.Minute))
	for key, bucket := range l.buckets {
		if bucket.lastSeen.Before(staleBefore) && !bucket.blockedUntil.After(now) {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) <= maxTrackedLoginBuckets {
		return
	}
	for len(l.buckets) > maxTrackedLoginBuckets {
		var oldestKey string
		var oldest time.Time
		for key, bucket := range l.buckets {
			if oldestKey == "" || bucket.lastSeen.Before(oldest) {
				oldestKey = key
				oldest = bucket.lastSeen
			}
		}
		delete(l.buckets, oldestKey)
	}
}

func loginKeys(ip, username string) []string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	normalizedUser := strings.ToLower(strings.TrimSpace(username))
	sum := sha256.Sum256([]byte(ip + "\x00" + normalizedUser))
	// The IP bucket slows broad guessing from one origin. The pair bucket adds
	// stricter tracking for a concrete account without allowing a remote client
	// to lock that account globally for every legitimate address.
	return []string{"ip:" + ip, "pair:" + hex.EncodeToString(sum[:])}
}

func remoteIP(r *http.Request) string {
	peer := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(peer)
	if err == nil && host != "" {
		peer = host
	}
	peerIP := net.ParseIP(strings.Trim(peer, "[]"))
	// Forwarded headers are accepted only from a loopback reverse proxy. This
	// supports a local Caddy/Nginx deployment without allowing remote clients
	// to spoof their address and bypass or weaponize the rate limiter.
	if peerIP != nil && peerIP.IsLoopback() {
		for _, candidate := range forwardedIPCandidates(r) {
			if ip := net.ParseIP(strings.TrimSpace(candidate)); ip != nil && !ip.IsUnspecified() {
				return ip.String()
			}
		}
	}
	if peerIP != nil {
		return peerIP.String()
	}
	return peer
}

func forwardedIPCandidates(r *http.Request) []string {
	// Read X-Forwarded-For from right to left. A correctly configured local
	// reverse proxy appends (or replaces with) the address it actually saw;
	// choosing the left-most value would allow a client-supplied prefix to
	// bypass rate limiting when proxy_add_x_forwarded_for is used.
	parts := make([]string, 0, 4)
	for _, raw := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(raw, ",") {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	items := make([]string, 0, len(parts)+1)
	for index := len(parts) - 1; index >= 0; index-- {
		items = append(items, parts[index])
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		items = append(items, value)
	}
	return items
}
