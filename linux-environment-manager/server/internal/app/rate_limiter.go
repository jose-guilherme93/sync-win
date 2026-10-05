package app

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	started time.Time
	count   int
}

type rateLimiter struct {
	mu          sync.Mutex
	entries     map[string]rateLimitEntry
	lastCleanup time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{entries: make(map[string]rateLimitEntry)}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	if l == nil || limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
		for existingKey, existing := range l.entries {
			if now.Sub(existing.started) > time.Hour {
				delete(l.entries, existingKey)
			}
		}
		l.lastCleanup = now
	}
	if len(l.entries) > 10000 {
		for existingKey := range l.entries {
			if len(l.entries) <= 9000 {
				break
			}
			delete(l.entries, existingKey)
		}
	}
	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.started) >= window {
		l.entries[key] = rateLimitEntry{started: now, count: 1}
		return true
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

// clientIP returns the address used for rate limiting. X-Forwarded-For and
// X-Real-IP are only trusted when the server is explicitly configured to sit
// behind a trusted proxy (LEM_TRUST_PROXY), so by default a client cannot evade
// the limiter by forging headers.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
				return first
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host
}

func requestRateKey(r *http.Request, bucket string, trustProxy bool) string {
	return bucket + ":" + clientIP(r, trustProxy)
}

func (s *Server) allowRate(w http.ResponseWriter, r *http.Request, bucket string, limit int, window time.Duration) bool {
	if s.rateLimiter == nil || s.rateLimiter.allow(requestRateKey(r, bucket, s.trustProxy), limit, window) {
		return true
	}
	w.Header().Set("Retry-After", "60")
	s.writeError(w, http.StatusTooManyRequests, errors.New("rate limit exceeded"))
	return false
}
