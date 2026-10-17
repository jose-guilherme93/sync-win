package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sync-win/server/internal/store"
)

func alertFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := newTestServer(t)
	user, err := s.store.CreateUser("alerts@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, session.Token
}

func alertRequest(t *testing.T, s *Server, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, req)
	return rec
}

func decodeAcks(t *testing.T, rec *httptest.ResponseRecorder) []store.AlertAck {
	t.Helper()
	var body struct {
		Acks []store.AlertAck `json:"acks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	return body.Acks
}

func TestAlertAcksStartEmpty(t *testing.T) {
	s, token := alertFixture(t)

	rec := alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if acks := decodeAcks(t, rec); len(acks) != 0 {
		t.Fatalf("expected no acks, got %+v", acks)
	}
	// The dashboard reads this array directly; null would make it crash on
	// iteration in a fresh account.
	if !strings.Contains(rec.Body.String(), `"acks":[]`) {
		t.Errorf("empty list must encode as [], got %s", rec.Body.String())
	}
}

func TestAlertAckIsStoredAndIdempotent(t *testing.T) {
	s, token := alertFixture(t)
	const id = "dev-1:cpu"

	rec := alertRequest(t, s, http.MethodPost, "/api/alerts", token, `{"id":"`+id+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var first store.AlertAck
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if first.ID != id || first.AckedAt == "" {
		t.Fatalf("ack = %+v", first)
	}

	rec = alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	acks := decodeAcks(t, rec)
	if len(acks) != 1 || acks[0].ID != id {
		t.Fatalf("acks = %+v", acks)
	}

	// A second ack must not duplicate the row or move its timestamp.
	rec = alertRequest(t, s, http.MethodPost, "/api/alerts", token, `{"id":"`+id+`"}`)
	var second store.AlertAck
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second ack: %v", err)
	}
	if second.AckedAt != first.AckedAt {
		t.Errorf("timestamp moved on re-ack: %s -> %s", first.AckedAt, second.AckedAt)
	}
	rec = alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	if acks := decodeAcks(t, rec); len(acks) != 1 {
		t.Fatalf("re-ack created %d rows", len(acks))
	}
}

func TestAlertAcksAreOwnerScoped(t *testing.T) {
	s, token := alertFixture(t)
	const id = "dev-1:memory"

	if rec := alertRequest(t, s, http.MethodPost, "/api/alerts", token, `{"id":"`+id+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("ack status = %d, body = %s", rec.Code, rec.Body.String())
	}

	other, err := s.store.CreateUser("other@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := s.store.CreateSession(other.ID)
	if err != nil {
		t.Fatal(err)
	}

	rec := alertRequest(t, s, http.MethodGet, "/api/alerts", otherSession.Token, "")
	if acks := decodeAcks(t, rec); len(acks) != 0 {
		t.Fatalf("another owner saw %+v", acks)
	}

	// Clearing one owner's acks must leave the other's alone.
	rec = alertRequest(t, s, http.MethodDelete, "/api/alerts", otherSession.Token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	rec = alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	if acks := decodeAcks(t, rec); len(acks) != 1 {
		t.Fatalf("owner acks were cleared by another account: %+v", acks)
	}
}

func TestAlertAckDeleteRemovesOneAndRestoreClearsAll(t *testing.T) {
	s, token := alertFixture(t)

	for _, id := range []string{"dev-1:cpu", "dev-1:disk:/"} {
		if rec := alertRequest(t, s, http.MethodPost, "/api/alerts", token, `{"id":"`+id+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("ack %s status = %d", id, rec.Code)
		}
	}

	rec := alertRequest(t, s, http.MethodDelete, "/api/alerts?id="+`dev-1:cpu`, token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unack status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	if acks := decodeAcks(t, rec); len(acks) != 1 || acks[0].ID != "dev-1:disk:/" {
		t.Fatalf("acks after unack = %+v", acks)
	}

	rec = alertRequest(t, s, http.MethodDelete, "/api/alerts", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d", rec.Code)
	}
	var cleared struct {
		Cleared int64 `json:"cleared"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if cleared.Cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared.Cleared)
	}
	rec = alertRequest(t, s, http.MethodGet, "/api/alerts", token, "")
	if acks := decodeAcks(t, rec); len(acks) != 0 {
		t.Fatalf("acks after restore = %+v", acks)
	}
}

func TestAlertAcksRejectInvalidIDs(t *testing.T) {
	s, token := alertFixture(t)

	for name, body := range map[string]string{
		"empty":      `{"id":""}`,
		"whitespace": `{"id":"   "}`,
		"control":    `{"id":"dev\u0000cpu"}`,
		"too long":   `{"id":"` + strings.Repeat("a", 400) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := alertRequest(t, s, http.MethodPost, "/api/alerts", token, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAlertAcksRequireSession(t *testing.T) {
	s, _ := alertFixture(t)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := alertRequest(t, s, method, "/api/alerts", "", `{"id":"dev-1:cpu"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, rec.Code)
		}
	}
}

func TestAlertAcksRejectUnsupportedMethod(t *testing.T) {
	s, token := alertFixture(t)

	rec := alertRequest(t, s, http.MethodPut, "/api/alerts", token, `{"id":"dev-1:cpu"}`)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
