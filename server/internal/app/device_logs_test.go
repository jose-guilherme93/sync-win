package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sync-win/server/internal/store"
)

// deviceLogsFixture builds a server with one owner, one device and a batch of
// journal lines already ingested.
type deviceLogsFixture struct {
	server   *Server
	deviceID string
	token    string
}

func newDeviceLogsFixture(t *testing.T, lines ...store.DeviceLog) deviceLogsFixture {
	t.Helper()
	s := newTestServer(t)
	user, err := s.store.CreateUser("logs@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-a", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) > 0 {
		if _, err := s.store.AppendDeviceLogs(device.ID, device.OwnerID, lines); err != nil {
			t.Fatal(err)
		}
	}
	return deviceLogsFixture{server: s, deviceID: device.ID, token: session.Token}
}

func (f deviceLogsFixture) get(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+f.deviceID+"/logs"+query, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.server.handleDeviceLogs(rec, req, f.deviceID)
	return rec
}

func decodePage(t *testing.T, rec *httptest.ResponseRecorder) store.DeviceLogPage {
	t.Helper()
	var page store.DeviceLogPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v (body %s)", err, rec.Body.String())
	}
	return page
}

func TestDeviceLogsEndpointReturnsStoredLines(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	f := newDeviceLogsFixture(t,
		store.DeviceLog{Timestamp: base.Format(time.RFC3339), Level: "error", Source: "kernel", Message: "EXT4-fs error"},
		store.DeviceLog{Timestamp: base.Add(time.Minute).Format(time.RFC3339), Level: "info", Source: "systemd", Message: "Started unit"},
	)

	rec := f.get(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	page := decodePage(t, rec)
	if page.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Total)
	}
	// Newest first.
	if page.Entries[0].Message != "Started unit" {
		t.Fatalf("first entry = %q, want the newest line", page.Entries[0].Message)
	}
	if page.Counts["error"] != 1 || page.Counts["info"] != 1 {
		t.Errorf("counts = %v, want one error and one info", page.Counts)
	}
	if len(page.Sources) != 2 {
		t.Errorf("sources = %v, want two", page.Sources)
	}
}

func TestDeviceLogsEndpointFilters(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	f := newDeviceLogsFixture(t,
		store.DeviceLog{Timestamp: base.Format(time.RFC3339), Level: "error", Source: "kernel", Message: "EXT4-fs error"},
		store.DeviceLog{Timestamp: base.Add(time.Minute).Format(time.RFC3339), Level: "warn", Source: "dockerd", Message: "iptables missing"},
		store.DeviceLog{Timestamp: base.Add(2 * time.Minute).Format(time.RFC3339), Level: "info", Source: "systemd", Message: "Started unit"},
	)

	for _, tc := range []struct {
		name    string
		query   string
		want    int
		wantAll int
	}{
		{"level", "?level=error", 1, 1},
		{"source", "?source=kernel", 1, 1},
		{"search", "?search=iptables", 1, 1},
		// Paging narrows the page, never the total: total counts everything
		// matching the filters so the toolbar can say "N of M".
		{"paging", "?limit=1&offset=1", 1, 3},
		{"since", "?since=" + base.Add(time.Minute).Format(time.RFC3339), 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := decodePage(t, f.get(t, tc.query))
			if len(page.Entries) != tc.want {
				t.Errorf("%s: entries = %d, want %d", tc.query, len(page.Entries), tc.want)
			}
			if page.Total != tc.wantAll {
				t.Errorf("%s: total = %d, want %d", tc.query, page.Total, tc.wantAll)
			}
		})
	}
}

func TestDeviceLogsEndpointRejectsMalformedSince(t *testing.T) {
	f := newDeviceLogsFixture(t)
	rec := f.get(t, "?since=yesterday")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestDeviceLogsEndpointIsolatesOwners(t *testing.T) {
	f := newDeviceLogsFixture(t,
		store.DeviceLog{Timestamp: "2026-10-07T12:00:00Z", Level: "info", Source: "systemd", Message: "owner A line"},
	)
	other, err := f.server.store.CreateUser("other@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := f.server.store.CreateSession(other.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+f.deviceID+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+otherSession.Token)
	rec := httptest.NewRecorder()
	f.server.handleDeviceLogs(rec, req, f.deviceID)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner status = %d, want 404", rec.Code)
	}
	if rec.Body.String() == "" {
		t.Error("cross-owner response is empty")
	}
}

func TestDeviceLogsEndpointMissingDeviceLooksIdenticalToForeign(t *testing.T) {
	f := newDeviceLogsFixture(t)

	// A real device belonging to somebody else.
	other, err := f.server.store.CreateUser("probe@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := f.server.store.CreateSession(other.ID)
	if err != nil {
		t.Fatal(err)
	}

	foreign := httptest.NewRequest(http.MethodGet, "/api/devices/"+f.deviceID+"/logs", nil)
	foreign.Header.Set("Authorization", "Bearer "+otherSession.Token)
	foreignRec := httptest.NewRecorder()
	f.server.handleDeviceLogs(foreignRec, foreign, f.deviceID)

	missing := httptest.NewRequest(http.MethodGet, "/api/devices/does-not-exist/logs", nil)
	missing.Header.Set("Authorization", "Bearer "+otherSession.Token)
	missingRec := httptest.NewRecorder()
	f.server.handleDeviceLogs(missingRec, missing, "does-not-exist")

	if foreignRec.Code != missingRec.Code {
		t.Fatalf("foreign = %d, missing = %d; a probe must not tell them apart",
			foreignRec.Code, missingRec.Code)
	}
	if foreignRec.Body.String() != missingRec.Body.String() {
		t.Fatalf("bodies differ: %s vs %s", foreignRec.Body.String(), missingRec.Body.String())
	}
}

func TestDeviceLogsEndpointRejectsNonGET(t *testing.T) {
	f := newDeviceLogsFixture(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/devices/"+f.deviceID+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.server.handleDeviceLogs(rec, req, f.deviceID)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// The original defect, exercised through the HTTP layer: a batch reported with
// telemetry must still be there after later telemetry posts that carry no logs.
func TestTelemetryIngestPersistsDeviceLogsAcrossCycles(t *testing.T) {
	s := newTestServer(t)
	// handleTelemetry feeds the downsampling aggregator, which newTestServer
	// leaves nil because no existing test exercises that path.
	s.aggregator = store.NewAggregator(s.store)
	user, err := s.store.CreateUser("telemetry@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-a", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	post := func(logs []store.DeviceLog) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"device_token": device.DeviceToken,
			"hardware": store.HardwareStats{
				CPUUsagePercent: 10,
				Logs:            logs,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/telemetry", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleTelemetry(rec, req, device.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("telemetry: %d %s", rec.Code, rec.Body.String())
		}
	}

	post([]store.DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "error", Source: "kernel", Message: "EXT4-fs error"},
	})

	// Five cycles later: the agent only samples every sixth, and each of these
	// posts overwrites hardware_json entirely.
	for i := 0; i < 5; i++ {
		post(nil)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	s.handleDeviceLogs(rec, req, device.ID)

	page := decodePage(t, rec)
	if page.Total != 1 {
		t.Fatalf("total = %d after five log-less cycles, want the reported line to survive", page.Total)
	}
	if page.Entries[0].Message != "EXT4-fs error" {
		t.Fatalf("surviving line = %q", page.Entries[0].Message)
	}
}

// Re-reporting the same journal window must not multiply stored rows.
func TestTelemetryIngestIsIdempotent(t *testing.T) {
	s := newTestServer(t)
	s.aggregator = store.NewAggregator(s.store)
	user, err := s.store.CreateUser("dedupe@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-a", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	batch := []store.DeviceLog{
		{Timestamp: "2026-10-07T12:00:00Z", Level: "info", Source: "systemd", Message: "Started unit"},
	}
	for i := 0; i < 3; i++ {
		payload, err := json.Marshal(map[string]any{
			"device_token": device.DeviceToken,
			"hardware":     store.HardwareStats{CPUUsagePercent: 10, Logs: batch},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/telemetry", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleTelemetry(rec, req, device.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("telemetry %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	s.handleDeviceLogs(rec, req, device.ID)

	if page := decodePage(t, rec); page.Total != 1 {
		t.Fatalf("total = %d after three identical windows, want 1", page.Total)
	}
}
