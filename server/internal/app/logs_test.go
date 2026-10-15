package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sync-win/server/internal/logging"
)

// logRow mirrors the JSON the logs API returns. It is deliberately not
// logging.LogEntry: that type marshals Level to a string but has no matching
// unmarshaler, so decoding a response into it fails.
type logRow struct {
	ID            int64  `json:"id"`
	Level         string `json:"level"`
	Category      string `json:"category"`
	Event         string `json:"event"`
	Message       string `json:"message"`
	RequestID     string `json:"request_id"`
	CorrelationID string `json:"correlation_id"`
}

type logPage struct {
	Entries []logRow `json:"entries"`
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

// sessionFor creates an account and returns a bearer token for it.
func sessionFor(t *testing.T, s *Server, email string) string {
	t.Helper()
	user, err := s.store.CreateUser(email, "correct horse battery staple")
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("create session %s: %v", email, err)
	}
	return session.Token
}

// seedLog writes one entry owned by owner. With no device id the store resolves
// owner_id from user_id, which is what makes the entry visible to that owner.
func seedLog(t *testing.T, s *Server, owner string, level logging.Level, cat logging.Category, event logging.Event, msg string) {
	t.Helper()
	err := s.logStore.WriteSingle(&logging.LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Category:  cat,
		Event:     event,
		Message:   msg,
		UserID:    owner,
	})
	if err != nil {
		t.Fatalf("seed log: %v", err)
	}
}

func getLogs(t *testing.T, s *Server, token, query string) (int, logPage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/logs"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.handleLogs(rec, req)

	var page logPage
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode logs page: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec.Code, page
}

func TestLogsListIsOwnerScoped(t *testing.T) {
	s := newTestServer(t)
	a := sessionFor(t, s, "a@example.com")
	b := sessionFor(t, s, "b@example.com")

	// Owners are keyed by user id, so seed via the ids the tokens map to.
	aOwner := ownerFor(t, s, a)
	bOwner := ownerFor(t, s, b)

	seedLog(t, s, aOwner, logging.LevelInfo, logging.CatSync, "sync_completed", "a-info")
	seedLog(t, s, aOwner, logging.LevelError, logging.CatHTTP, "http_error", "a-error")
	seedLog(t, s, bOwner, logging.LevelError, logging.CatHTTP, "http_error", "b-secret")

	code, page := getLogs(t, s, a, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want 2 (only owner A's entries)", page.Total)
	}
	for _, e := range page.Entries {
		if e.Message == "b-secret" {
			t.Fatal("owner B's entry leaked into owner A's page")
		}
	}
}

func TestLogsFiltersByLevelCategoryAndSearch(t *testing.T) {
	s := newTestServer(t)
	token := sessionFor(t, s, "filter@example.com")
	owner := ownerFor(t, s, token)

	seedLog(t, s, owner, logging.LevelInfo, logging.CatSync, "sync_completed", "ordinary line")
	seedLog(t, s, owner, logging.LevelError, logging.CatHTTP, "http_error", "the needle is here")

	_, byLevel := getLogs(t, s, token, "?level=ERROR")
	if byLevel.Total != 1 || byLevel.Entries[0].Message != "the needle is here" {
		t.Fatalf("level filter returned %+v", byLevel.Entries)
	}

	_, byCategory := getLogs(t, s, token, "?category=sync")
	if byCategory.Total != 1 || byCategory.Entries[0].Event != "sync_completed" {
		t.Fatalf("category filter returned %+v", byCategory.Entries)
	}

	_, bySearch := getLogs(t, s, token, "?search=needle")
	if bySearch.Total != 1 || bySearch.Entries[0].Level != "ERROR" {
		t.Fatalf("search filter returned %+v", bySearch.Entries)
	}

	_, none := getLogs(t, s, token, "?search=nothing-matches-this")
	if none.Total != 0 {
		t.Fatalf("unexpected matches: %+v", none.Entries)
	}
}

func TestLogsPaginationAndLimitCap(t *testing.T) {
	s := newTestServer(t)
	token := sessionFor(t, s, "page@example.com")
	owner := ownerFor(t, s, token)

	for i := 0; i < 3; i++ {
		seedLog(t, s, owner, logging.LevelInfo, logging.CatSystem, logging.EventAppStarted, fmt.Sprintf("entry-%d", i))
	}

	_, first := getLogs(t, s, token, "?limit=2&offset=0")
	if first.Total != 3 || len(first.Entries) != 2 {
		t.Fatalf("first page: total=%d entries=%d", first.Total, len(first.Entries))
	}
	_, second := getLogs(t, s, token, "?limit=2&offset=2")
	if len(second.Entries) != 1 {
		t.Fatalf("second page entries = %d, want 1", len(second.Entries))
	}

	// An oversized limit is clamped, not honored: the handler must not let a
	// caller pull the whole table in one request.
	_, capped := getLogs(t, s, token, "?limit=100000")
	if capped.Limit != 100 {
		t.Fatalf("limit = %d, want the clamped 100", capped.Limit)
	}
}

func TestLogsRequiresSession(t *testing.T) {
	s := newTestServer(t)
	if code, _ := getLogs(t, s, "", ""); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
}

func TestLogsRejectsWriteMethods(t *testing.T) {
	s := newTestServer(t)
	token := sessionFor(t, s, "method@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleLogs(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestAuditAndErrorsViewsFilterByLevel(t *testing.T) {
	s := newTestServer(t)
	token := sessionFor(t, s, "views@example.com")
	owner := ownerFor(t, s, token)

	seedLog(t, s, owner, logging.LevelInfo, logging.CatSync, "sync_completed", "info-line")
	seedLog(t, s, owner, logging.LevelError, logging.CatHTTP, "http_error", "error-line")
	seedLog(t, s, owner, logging.LevelAudit, logging.CatAudit, "command_dispatched", "audit-line")

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleAudit(rec, req)
	var audit logPage
	if err := json.Unmarshal(rec.Body.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Total != 1 || audit.Entries[0].Message != "audit-line" {
		t.Fatalf("audit view returned %+v", audit.Entries)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/errors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.handleErrors(rec, req)
	var errs logPage
	if err := json.Unmarshal(rec.Body.Bytes(), &errs); err != nil {
		t.Fatal(err)
	}
	if errs.Total != 1 || errs.Entries[0].Message != "error-line" {
		t.Fatalf("errors view returned %+v", errs.Entries)
	}
}

// ownerFor resolves the owner id behind a session token using the same lookup
// the handlers use, so the test does not assume a token format.
func ownerFor(t *testing.T, s *Server, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	owner, ok := s.requireSession(httptest.NewRecorder(), req)
	if !ok || owner == "" {
		t.Fatal("could not resolve owner for session")
	}
	return owner
}
