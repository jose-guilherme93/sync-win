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
			AgentMemoryBytes: 12_000_000,
			AgentCPUUsage:    0.41,
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
