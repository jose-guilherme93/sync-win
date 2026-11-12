package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sync-win/server/internal/store"
)

func TestAgentUsageEndpointReturnsOneSeriesPerDevice(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("usage@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-usage", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(telemetryRequest{
		DeviceToken: device.DeviceToken,
		Hardware: store.HardwareStats{
			CPUUsagePercent:  10,
			MemoryTotalBytes: 16 << 30,
			AgentMemoryBytes: ptrUint64(12_000_000),
			AgentCPUUsage:    ptrFloat64(0.41),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/telemetry", bytes.NewReader(body))
	post.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleTelemetry(rec, post, device.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("telemetry: %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/usage", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	out := httptest.NewRecorder()
	s.handleAgentUsage(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", out.Code, out.Body.String())
	}

	var payload struct {
		From    string                             `json:"from"`
		To      string                             `json:"to"`
		Devices map[string][]store.AgentUsagePoint `json:"devices"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.From == "" || payload.To == "" {
		t.Error("the response must name the window it covers")
	}
	points := payload.Devices[device.ID]
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1", len(points))
	}
	if points[0].MemoryBytes != 12_000_000 || points[0].CPUPercent != 0.41 {
		t.Fatalf("point = %+v", points[0])
	}
}

func TestAgentUsageEndpointRequiresASession(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/usage", nil)
	rec := httptest.NewRecorder()
	s.handleAgentUsage(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAgentUsageEndpointRejectsABadWindow(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("usage2@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/usage?from=yesterday&to=now", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	s.handleAgentUsage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestStorageHistoryClampsAndReportsTheEffectiveWindow(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("storage@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-storage", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/storage/history?days=1000", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	s.handleStorageHistory(rec, req, device.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Days int `json:"days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Days != 400 {
		t.Fatalf("days = %d, want the clamped 400 the store actually honors", payload.Days)
	}
}

// The daily rollup answers a once-a-day question; writing it on every heartbeat
// was WAL churn with no reader.
func TestStorageRollupIsThrottledPerDevice(t *testing.T) {
	s := newTestServer(t)
	if !s.shouldRollupStorage("dev-1") {
		t.Fatal("the first sample must roll up")
	}
	if s.shouldRollupStorage("dev-1") {
		t.Fatal("a second sample inside the throttle window must not write")
	}
	if !s.shouldRollupStorage("dev-2") {
		t.Fatal("the throttle is per device")
	}
}

func ptrFloat64(value float64) *float64 { return &value }
func ptrUint64(value uint64) *uint64    { return &value }
