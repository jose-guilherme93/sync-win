package app

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// The dev Vite proxy rewrites the Host header to the compose service, so a
// browser opening the dashboard over the LAN or Tailscale has an Origin that
// cannot match the request host. Development must accept that Origin; a
// production server must not start trusting arbitrary origins because of it.
func TestTerminalOriginAllowsTheBrowserHostInDevelopment(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("SYNCWIN_ENV", "development")
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/devices/dev-1/terminal", nil)
	req.Host = "server:8080"
	req.Header.Set("Origin", "http://100.115.101.127:5273")

	opts := s.terminalAcceptOptions(req)
	if !slices.Contains(opts.OriginPatterns, "100.115.101.127:5273") {
		t.Fatalf("origin patterns = %v, want the browser origin", opts.OriginPatterns)
	}
}

func TestTerminalOriginDoesNotTrustTheBrowserHostInProduction(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("SYNCWIN_ENV", "production")
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/devices/dev-1/terminal", nil)
	req.Host = "server:8080"
	req.Header.Set("Origin", "http://100.115.101.127:5273")

	opts := s.terminalAcceptOptions(req)
	if slices.Contains(opts.OriginPatterns, "100.115.101.127:5273") {
		t.Fatalf("production trusted the browser origin: %v", opts.OriginPatterns)
	}
}
