package app

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	limiter := newRateLimiter()
	if !limiter.allow("test", 2, time.Minute) {
		t.Fatal("first request should be allowed")
	}
	if !limiter.allow("test", 2, time.Minute) {
		t.Fatal("second request should be allowed")
	}
	if limiter.allow("test", 2, time.Minute) {
		t.Fatal("third request should be rejected")
	}
}

func TestRequestRateKeyIgnoresPort(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	if got := requestRateKey(r, "auth", false); got != "auth:192.0.2.10" {
		t.Fatalf("rate key = %q", got)
	}
}

func TestRequestRateKeyTrustsProxyOnlyWhenConfigured(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")

	// Default: the header is ignored so a client cannot spoof its identity.
	if got := requestRateKey(r, "auth", false); got != "auth:203.0.113.9" {
		t.Fatalf("untrusted rate key = %q", got)
	}
	// Behind a trusted proxy the forwarded client address is used.
	if got := requestRateKey(r, "auth", true); got != "auth:198.51.100.7" {
		t.Fatalf("trusted rate key = %q", got)
	}
}
