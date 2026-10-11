package logging

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"
)

// requestIDHeader is the header used to propagate request IDs.
const requestIDHeader = "X-Request-ID"

// correlationIDHeader is the header used to propagate correlation IDs.
const correlationIDHeader = "X-Correlation-ID"

// HTTPMiddleware creates an HTTP middleware that logs requests.
func HTTPMiddleware(logger *Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Extract or generate request ID
		requestID := r.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Extract or generate correlation ID
		correlationID := r.Header.Get(correlationIDHeader)
		if correlationID == "" {
			correlationID = requestID
		}

		// Set response header
		w.Header().Set(requestIDHeader, requestID)
		w.Header().Set(correlationIDHeader, correlationID)

		// Wrap response writer to capture status
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Never persist bearer-like secrets carried in a URL path. Enrollment
		// scripts use /install/{token}; the raw path is still used internally
		// for routing decisions, but logs receive a redacted form.
		loggedPath := redactPath(r.URL.Path)

		// Create request-scoped logger
		reqLogger := &requestLogger{
			logger:        logger,
			requestID:     requestID,
			correlationID: correlationID,
			startTime:     start,
			method:        r.Method,
			path:          loggedPath,
			remoteAddr:    r.RemoteAddr,
		}

		// Skip noise paths for detailed logging
		skipDetailed := isNoisePath(r.URL.Path) || r.Method == "OPTIONS"

		// Call next handler
		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		status := rw.statusCode

		// Always log to access aggregation
		reqLogger.logAccess(status, duration)

		// Smart event logging
		if !skipDetailed || shouldPersistHTTP(r.Method, r.URL.Path, status, duration.Milliseconds()) {
			reqLogger.logEvent(status, duration)
		}
	})
}

func redactPath(path string) string {
	if strings.HasPrefix(path, "/install/") {
		return "/install/[REDACTED]"
	}
	return path
}

type requestLogger struct {
	logger        *Logger
	requestID     string
	correlationID string
	startTime     time.Time
	method        string
	path          string
	remoteAddr    string
}

func (rl *requestLogger) logAccess(status int, duration time.Duration) {
	// This could aggregate into http_access table in the future
	_ = status
	_ = duration
}

func (rl *requestLogger) logEvent(status int, duration time.Duration) {
	level := LevelDebug
	category := CatHTTP
	event := Event("http_request")

	durationMs := duration.Milliseconds()

	// A long-lived stream is not a slow request. The notification SSE endpoint
	// stays open for hours by design, so timing it produced a permanent
	// http_slow_request entry with a duration in the millions of milliseconds
	// that buried the genuine ones.
	if isStreamingPath(rl.path) {
		return
	}

	// Determine level and event based on status
	switch {
	case status >= 500:
		level = LevelError
		event = EventHTTPError
	case status >= 400:
		level = LevelWarn
		if status == 401 || status == 403 {
			level = LevelWarn
			event = EventAuthDeny
		}
	case status >= 300:
		level = LevelInfo
	case durationMs > 1000:
		level = LevelNotice
		event = EventHTTPSlowRequest
	}

	meta := map[string]any{
		"method":      rl.method,
		"path":        rl.path,
		"status":      status,
		"duration_ms": durationMs,
		"remote":      rl.remoteAddr,
	}

	// Extract device_id from path if present
	if deviceID := extractDeviceID(rl.path); deviceID != "" {
		meta["device_id"] = deviceID
	}

	// Check if this is an auth endpoint
	if strings.HasPrefix(rl.path, "/api/auth/") {
		category = CatAuth
	}

	rl.logger.log(level, category, event, "", meta, rl.requestID, rl.correlationID)
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher for SSE support.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap allows http.ResponseController to work correctly.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Hijack forwards to the underlying writer so WebSocket upgrades work through
// this middleware. Without it the upgrade finds no Hijacker and fails.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// extractDeviceID extracts a device ID from a URL path like /api/devices/{id}/...
func extractDeviceID(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "devices" && i+1 < len(parts) {
			next := parts[i+1]
			if strings.HasPrefix(next, "dev-") || strings.HasPrefix(next, "et-") {
				return next
			}
		}
	}
	return ""
}

// LogHTTPRequest is a convenience function for manual HTTP logging.
func LogHTTPRequest(logger *Logger, method, path, remoteAddr string, status int, duration time.Duration) {
	meta := map[string]any{
		"method":      method,
		"path":        path,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
		"remote":      remoteAddr,
	}

	level := LevelInfo
	if status >= 500 {
		level = LevelError
	} else if status >= 400 {
		level = LevelWarn
	}

	logger.Log(level, CatHTTP, Event("http_request"), "", meta)
}

// LogWriter captures writes and logs them.
type LogWriter struct {
	logger *Logger
	buf    bytes.Buffer
}

func NewLogWriter(logger *Logger) *LogWriter {
	return &LogWriter{logger: logger}
}

func (lw *LogWriter) Write(p []byte) (n int, err error) {
	return lw.buf.Write(p)
}

func (lw *LogWriter) Flush(level Level, cat Category, event Event) {
	if lw.buf.Len() > 0 {
		lw.logger.Log(level, cat, event, lw.buf.String(), nil)
		lw.buf.Reset()
	}
}
