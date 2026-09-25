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
	if got := requestRateKey(r, "auth"); got != "auth:192.0.2.10" {
		t.Fatalf("rate key = %q", got)
	}
}
