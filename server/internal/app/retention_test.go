package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// retentionBody mirrors the documented wire format. It is deliberately not the
// store type: the bug this test pins was a mismatch between the store's JSON
// tags and what the API/UI send.
type retentionBody struct {
	RawHours         int `json:"raw_hours"`
	Resolution1mDays int `json:"resolution_1m_days"`
	Resolution5mDays int `json:"resolution_5m_days"`
	Resolution1hDays int `json:"resolution_1h_days"`
}

func retentionRequest(t *testing.T, s *Server, method, token, body string) (*httptest.ResponseRecorder, retentionBody) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/retention", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.handleRetention(rec, req)

	var decoded retentionBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec, decoded
}

func TestRetentionRoundTripUsesTheDocumentedJSON(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("retention@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	rec, defaults := retentionRequest(t, s, http.MethodGet, session.Token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if defaults != (retentionBody{RawHours: 2, Resolution1mDays: 7, Resolution5mDays: 30, Resolution1hDays: 365}) {
		t.Fatalf("defaults = %+v", defaults)
	}

	// The exact body the Settings screen sends.
	const update = `{"raw_hours":6,"resolution_1m_days":14,"resolution_5m_days":60,"resolution_1h_days":730}`
	rec, saved := retentionRequest(t, s, http.MethodPut, session.Token, update)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if saved.Resolution1mDays != 14 || saved.Resolution5mDays != 60 || saved.Resolution1hDays != 730 {
		t.Fatalf("saved = %+v", saved)
	}

	// And it must survive the next read, not just the echo.
	_, reread := retentionRequest(t, s, http.MethodGet, session.Token, "")
	if reread != saved {
		t.Fatalf("reread = %+v, want %+v", reread, saved)
	}
}

func TestRetentionRejectsOutOfRangeValues(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("retention-bad@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"zero raw":           `{"raw_hours":0,"resolution_1m_days":7,"resolution_5m_days":30,"resolution_1h_days":365}`,
		"raw too long":       `{"raw_hours":169,"resolution_1m_days":7,"resolution_5m_days":30,"resolution_1h_days":365}`,
		"1m zero (untagged)": `{"resolution_1m_days":0,"raw_hours":2,"resolution_5m_days":30,"resolution_1h_days":365}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec, _ := retentionRequest(t, s, http.MethodPut, session.Token, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRetentionRequiresSession(t *testing.T) {
	s := newTestServer(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec, _ := retentionRequest(t, s, method, "", `{"raw_hours":2,"resolution_1m_days":7,"resolution_5m_days":30,"resolution_1h_days":365}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, rec.Code)
		}
	}
}
