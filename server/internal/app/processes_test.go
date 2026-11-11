package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sync-win/server/internal/logging"
	"sync-win/server/internal/store"
)

func processFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s := newTestServer(t)
	ownerID, token, deviceID := newAuthedDevice(t, s, "processes@example.com")
	if err := s.store.UpdateProcessState(deviceID, store.ProcessSnapshot{
		Processes: []store.ProcessRow{
			{PID: 1000, PPID: 1, Name: "firefox", CPUPercent: 12.5, RSSBytes: 2048, StartTimeUnix: 1005},
			{PID: 1001, PPID: 1, Name: "bash", CPUPercent: 0.5, RSSBytes: 1024, StartTimeUnix: 1006},
		},
		Stats:       &store.ProcessStats{Total: 2, Load1: 0.4},
		CollectedAt: "2026-11-11T10:00:00Z",
		IOAvailable: true,
		Collected:   true,
	}); err != nil {
		t.Fatal(err)
	}
	_ = ownerID
	return s, token, deviceID
}

func processRequest(s *Server, method, path, token, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleDeviceDetail(rec, req)
	return rec
}

func TestProcessSignalRequiresOwnerSession(t *testing.T) {
	s, token, deviceID := processFixture(t)
	body := `{"signal":"TERM"}`

	if rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+"/processes/1000/signal", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rec.Code)
	}
	if rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+"/processes/1000/signal", token, body); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("gate off = %d, want 503", rec.Code)
	}

	other, otherToken, _ := func() (string, string, string) {
		user, err := s.store.CreateUser("other-process@example.com", "correct horse battery staple")
		if err != nil {
			t.Fatal(err)
		}
		session, err := s.store.CreateSession(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		device, err := s.store.RegisterDevice("other-proc-pc", "", user.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return user.ID, session.Token, device.ID
	}()
	_ = other
	s.flags.EnableRemoteMutations = true
	if rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+"/processes/1000/signal", otherToken, body); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign owner = %d, want 404", rec.Code)
	}
}

func TestProcessSignalValidation(t *testing.T) {
	s, token, deviceID := processFixture(t)
	s.flags.EnableRemoteMutations = true

	cases := []struct {
		name string
		path string
		body string
	}{
		{"unknown field", "/processes/1000/signal", `{"signal":"TERM","extra":true}`},
		{"signal outside allowlist", "/processes/1000/signal", `{"signal":"HUP"}`},
		{"bad pid", "/processes/1/signal", `{"signal":"TERM"}`},
		{"pid not in snapshot", "/processes/4242/signal", `{"signal":"TERM"}`},
		{"nice out of range", "/processes/1000/nice", `{"nice":-100}`},
		{"ionice bad class", "/processes/1000/ionice", `{"class":99,"level":0}`},
		{"ionice bad level", "/processes/1000/ionice", `{"class":2,"level":9}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+tc.path, token, tc.body)
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
				t.Fatalf("%s = %d, want 400/404: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
	if rec := processRequest(s, http.MethodGet, "/api/devices/"+deviceID+"/processes/1000/signal", token, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET signal = %d, want 405", rec.Code)
	}
}

func TestProcessSignalQueuesWithSnapshotStartTime(t *testing.T) {
	s, token, deviceID := processFixture(t)
	s.flags.EnableRemoteMutations = true

	rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+"/processes/1000/signal", token, `{"signal":"TERM"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("queue signal = %d: %s", rec.Code, rec.Body.String())
	}
	var command store.DeviceCommand
	if err := json.Unmarshal(rec.Body.Bytes(), &command); err != nil {
		t.Fatal(err)
	}
	if command.Type != "process_signal" || command.Status != "queued" {
		t.Fatalf("command = %+v", command)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(command.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["signal"] != "TERM" {
		t.Fatalf("payload = %v", payload)
	}
	if payload["start_time_unix"] != float64(1005) {
		t.Fatalf("start_time_unix = %v, want 1005 from the snapshot", payload["start_time_unix"])
	}
}

// The dashboard client sends start_time_unix in the body; the server compares
// it against its own snapshot. Without this the strict decoder rejected every
// real request with 400 while the tests below only ever sent {"signal":...}.
func TestProcessMutationAcceptsWebClientBodies(t *testing.T) {
	s, token, deviceID := processFixture(t)
	s.flags.EnableRemoteMutations = true

	cases := []struct {
		name string
		path string
		body string
	}{
		{"signal", "/processes/1000/signal", `{"signal":"TERM","start_time_unix":1005}`},
		{"signal KILL", "/processes/1001/signal", `{"signal":"KILL","start_time_unix":1006}`},
		{"nice", "/processes/1000/nice", `{"nice":10,"start_time_unix":1005}`},
		{"ionice", "/processes/1000/ionice", `{"class":2,"level":7,"start_time_unix":1005}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+tc.path, token, tc.body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s = %d, want 202: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// A start time that no longer matches the snapshot means the pid was reused
// (or the tab is stale): the command must not be queued.
func TestProcessMutationRejectsStaleStartTime(t *testing.T) {
	s, token, deviceID := processFixture(t)
	s.flags.EnableRemoteMutations = true

	cases := []struct {
		name string
		path string
		body string
	}{
		{"signal", "/processes/1000/signal", `{"signal":"TERM","start_time_unix":999}`},
		{"nice", "/processes/1000/nice", `{"nice":10,"start_time_unix":999}`},
		{"ionice", "/processes/1000/ionice", `{"class":2,"level":7,"start_time_unix":999}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+tc.path, token, tc.body)
			if rec.Code != http.StatusConflict {
				t.Fatalf("%s = %d, want 409: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestProcessSignalAttemptsAreAudited(t *testing.T) {
	s, token, deviceID := processFixture(t)
	s.flags.EnableRemoteMutations = true

	if rec := processRequest(s, http.MethodPost, "/api/devices/"+deviceID+"/processes/1000/signal", token, `{"signal":"HUP"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("denied attempt = %d", rec.Code)
	}
	s.log.Flush()
	result, err := s.logStore.Query(logging.QueryParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range result.Entries {
		if entry.Event != logging.EventCmdRejected && entry.Event != logging.EventCmdCreated {
			continue
		}
		if entry.Metadata == nil {
			continue
		}
		if entry.Metadata["process_kind"] == "signal" || entry.Metadata["pid"] == float64(1000) || entry.Metadata["pid"] == 1000 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no process audit entry found in %d entries", len(result.Entries))
	}
}

func TestProcessesEndpointAuthAndShape(t *testing.T) {
	s, token, deviceID := processFixture(t)

	if rec := processRequest(s, http.MethodGet, "/api/devices/"+deviceID+"/processes", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rec.Code)
	}
	rec := processRequest(s, http.MethodGet, "/api/devices/"+deviceID+"/processes", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET processes = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Processes      []store.ProcessRow  `json:"processes"`
		Stats          *store.ProcessStats `json:"stats"`
		CollectedAt    string              `json:"collected_at"`
		IOAvailable    bool                `json:"io_available"`
		SignalsEnabled bool                `json:"signals_enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Processes) != 2 || body.Processes[0].Name != "firefox" {
		t.Fatalf("processes = %+v", body.Processes)
	}
	if body.Stats == nil || body.Stats.Total != 2 {
		t.Fatalf("stats = %+v", body.Stats)
	}
	if !body.IOAvailable || body.SignalsEnabled {
		t.Fatalf("io_available/signals_enabled = %v/%v", body.IOAvailable, body.SignalsEnabled)
	}
	s.flags.EnableRemoteMutations = true
	rec = processRequest(s, http.MethodGet, "/api/devices/"+deviceID+"/processes", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.SignalsEnabled {
		t.Fatal("signals_enabled must follow the mutation gate")
	}
}

func TestProcessHistoryEndpoint(t *testing.T) {
	s, token, deviceID := processFixture(t)
	now := time.Now().UTC()
	if err := s.store.RecordProcessHistory(deviceID, store.ProcessHistory{
		IntervalSeconds: 60,
		Samples:         []store.ProcessHistorySample{{T: now.Unix(), Rows: []store.ProcessHistoryPoint{{PID: 1000, Name: "firefox", CPU: 12.5, RSS: 2048}}}},
		Events:          []store.ProcessHistoryEvent{{T: now.Unix(), Type: "started", PID: 1001, Name: "vim"}},
	}, now); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		query string
		code  int
	}{{"?range=1h", http.StatusOK}, {"?range=24h", http.StatusOK}, {"?range=7d", http.StatusOK}, {"?range=90d", http.StatusBadRequest}, {"", http.StatusBadRequest}} {
		rec := processRequest(s, http.MethodGet, "/api/devices/"+deviceID+"/processes/history"+tc.query, token, "")
		if rec.Code != tc.code {
			t.Fatalf("history%s = %d, want %d: %s", tc.query, rec.Code, tc.code, rec.Body.String())
		}
		if tc.code == http.StatusOK {
			var body store.ProcessHistoryResult
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Series) == 0 || len(body.Buckets) == 0 {
				t.Fatalf("history body empty: %+v", body)
			}
		}
	}
}
