package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"sync-win/server/internal/store"
)

// GET /api/devices/{id}/telemetry/series is the compact chart endpoint: only
// the series leave the server, never the raw telemetry payload.

func seriesSeriesGet(t *testing.T, s *Server, token, deviceID, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/telemetry/series"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleTelemetrySeries(rec, req, deviceID)
	return rec
}

func seedTelemetryPost(t *testing.T, s *Server, device store.Device, hardware store.HardwareStats) {
	t.Helper()
	body, err := json.Marshal(telemetryRequest{DeviceToken: device.DeviceToken, Hardware: hardware})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleTelemetry(rec, req, device.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("telemetry post status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestTelemetrySeriesReturnsSeriesOnly(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("series@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-series", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	seedTelemetryPost(t, s, device, store.HardwareStats{
		CPUUsagePercent:  14.2,
		MemoryUsedBytes:  621 * 1024 * 1024,
		MemoryTotalBytes: 1000 * 1024 * 1024,
		CPUTemperature:   52,
		OperatingSystem:  "Fedora Linux",
	})

	rec := seriesSeriesGet(t, s, newSessionToken(t, s, user.ID), device.ID, "?window=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"window", "resolution", "from", "to", "points"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("response missing %q: %s", key, rec.Body.String())
		}
	}
	if payload["window"] != "1h" {
		t.Fatalf("window = %v, want 1h", payload["window"])
	}
	points, ok := payload["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("points = %v, want exactly one", payload["points"])
	}
	point, ok := points[0].(map[string]any)
	if !ok {
		t.Fatalf("point shape unexpected: %v", points[0])
	}
	if len(point) != 7 {
		t.Fatalf("point has %d fields, want exactly t,cpu,mem,disk,rx,tx,temp: %v", len(point), point)
	}
	for _, key := range []string{"t", "cpu", "mem", "disk", "rx", "tx", "temp"} {
		if _, ok := point[key]; !ok {
			t.Fatalf("point missing %q: %v", key, point)
		}
	}
	// The original payload blob must never ride along.
	for _, leaked := range []string{"operating_system", "cpu_usage_percent", "logs", "collected_at"} {
		if bytes.Contains(rec.Body.Bytes(), []byte(leaked)) {
			t.Fatalf("original payload field %q leaked into series response: %s", leaked, rec.Body.String())
		}
	}
	if mem, ok := point["mem"].(float64); !ok || mem < 62 || mem > 62.2 {
		t.Fatalf("mem = %v, want ~62.1", point["mem"])
	}
	if cpu, ok := point["cpu"].(float64); !ok || cpu != 14.2 {
		t.Fatalf("cpu = %v, want 14.2", point["cpu"])
	}
}

func TestTelemetrySeriesSinceReturnsOnlyDelta(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("since@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-since", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	token := newSessionToken(t, s, user.ID)

	// Three real telemetry posts, one second apart so their timestamps differ
	// (received_at is stored with second precision).
	for i := 0; i < 3; i++ {
		seedTelemetryPost(t, s, device, store.HardwareStats{CPUUsagePercent: float64(i)})
		if i < 2 {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	rec := seriesSeriesGet(t, s, token, device.ID, "?window=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var full struct {
		Points []struct {
			T   int64    `json:"t"`
			CPU *float64 `json:"cpu"`
		} `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Points) != 3 {
		t.Fatalf("full window points = %d, want 3", len(full.Points))
	}
	first := full.Points[0].T

	var delta struct {
		Points []struct {
			T   int64    `json:"t"`
			CPU *float64 `json:"cpu"`
		} `json:"points"`
	}
	rec = seriesSeriesGet(t, s, token, device.ID, "?window=1h&since="+strconv.FormatInt(first, 10))
	if rec.Code != http.StatusOK {
		t.Fatalf("since status = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &delta); err != nil {
		t.Fatal(err)
	}
	if len(delta.Points) != 2 {
		t.Fatalf("delta points = %d, want 2", len(delta.Points))
	}
	for _, p := range delta.Points {
		if p.T <= first {
			t.Fatalf("point t = %d not greater than since = %d", p.T, first)
		}
	}
	if delta.Points[0].CPU == nil || *delta.Points[0].CPU != 1 {
		t.Fatalf("first delta cpu = %v, want 1", delta.Points[0].CPU)
	}
}

func TestTelemetrySeriesCapsPointsWithStride(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("cap@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-cap", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	token := newSessionToken(t, s, user.ID)

	payload := []byte(`{"cpu_usage_percent":5.0,"memory_used_bytes":1,"memory_total_bytes":2}`)
	for i := 0; i < 1200; i++ {
		if err := s.store.AppendTelemetry(device.ID, payload); err != nil {
			t.Fatal(err)
		}
	}

	rec := seriesSeriesGet(t, s, token, device.ID, "?window=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Points []struct {
			T int64 `json:"t"`
		} `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Points) > 900 {
		t.Fatalf("points = %d, must never exceed the 900 cap", len(resp.Points))
	}
	if len(resp.Points) == 0 {
		t.Fatal("strided response must not be empty")
	}
	for i := 1; i < len(resp.Points); i++ {
		if resp.Points[i].T < resp.Points[i-1].T {
			t.Fatalf("points out of order after stride: %d then %d", resp.Points[i-1].T, resp.Points[i].T)
		}
	}
}

func TestTelemetrySeriesRejectsUnknownWindow(t *testing.T) {
	s := newTestServer(t)
	_, token, deviceID := newAuthedDevice(t, s, "window@example.com")

	for _, query := range []string{"?window=5h", "?window=", ""} {
		rec := seriesSeriesGet(t, s, token, deviceID, query)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("query %q: status = %d, want 400", query, rec.Code)
		}
	}
	rec := seriesSeriesGet(t, s, token, deviceID, "?window=1h&since=not-a-number")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid since: status = %d, want 400", rec.Code)
	}
}

func TestTelemetrySeriesEnforcesDeviceOwnership(t *testing.T) {
	s := newTestServer(t)
	owner, err := s.store.CreateUser("series-owner@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-owned", owner.ID, owner.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	other, err := s.store.CreateUser("intruder@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherToken := newSessionToken(t, s, other.ID)

	rec := seriesSeriesGet(t, s, otherToken, device.ID, "?window=1h")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other owner status = %d, want 403", rec.Code)
	}

	rec = seriesSeriesGet(t, s, otherToken, "no-such-device", "?window=1h")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing device status = %d, want 404", rec.Code)
	}

	// No authentication at all is not a public route.
	rec = seriesSeriesGet(t, s, "", device.ID, "?window=1h")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous status = %d, want 403", rec.Code)
	}
}

// newSessionToken opens a dashboard session the way the login handler does.
func newSessionToken(t *testing.T, s *Server, userID string) string {
	t.Helper()
	session, err := s.store.CreateSession(userID)
	if err != nil {
		t.Fatal(err)
	}
	return session.Token
}
