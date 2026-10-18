package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"sync-win/server/internal/logging"
)

func logStreamFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s := newTestServer(t)
	user, err := s.store.CreateUser("stream@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, user.ID, session.Token
}

func writeTestLog(t *testing.T, s *Server, ownerID string, level logging.Level, event logging.Event, message string) {
	t.Helper()
	entry := &logging.LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Category:  logging.CatSystem,
		Event:     event,
		Message:   message,
		UserID:    ownerID,
	}
	if err := s.logStore.WriteSingle(entry); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

// runLogStream connects to the handler, runs during, and returns what the
// stream wrote until it is canceled. The handler must have returned before
// the recorder is read: httptest.ResponseRecorder is not safe for concurrent
// use.
func runLogStream(t *testing.T, s *Server, token, query string, during func()) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleLogStream(rec, req)
		close(done)
	}()

	// Let the handler write its headers and enter the polling loop.
	time.Sleep(50 * time.Millisecond)
	during()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	return rec.Body.String()
}

func issueStreamTicket(t *testing.T, s *Server, ownerID string) string {
	t.Helper()
	ticket, err := s.streamTickets.issue(ownerID)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestLogStreamRequiresValidTicket(t *testing.T) {
	s, ownerID, token := logStreamFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleLogStream(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no ticket: status = %d, want 401", rec.Code)
	}

	// A ticket is single use: the notification stream already consumes it
	// before the log stream can.
	ticket := issueStreamTicket(t, s, ownerID)
	if !s.streamTickets.consume(ticket, ownerID) {
		t.Fatal("ticket was not valid")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/logs/stream?ticket="+ticket, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.handleLogStream(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused ticket: status = %d, want 401", rec.Code)
	}
}

func TestLogStreamTokenRequiresSession(t *testing.T) {
	s, _, token := logStreamFixture(t)

	rec := httptest.NewRecorder()
	s.handleLogStreamToken(rec, httptest.NewRequest(http.MethodPost, "/api/logs/stream-token", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/logs/stream-token", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.handleLogStreamToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ticket"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestLogStreamPushesNewEntries(t *testing.T) {
	s, ownerID, token := logStreamFixture(t)
	ticket := issueStreamTicket(t, s, ownerID)

	body := runLogStream(t, s, token, "ticket="+ticket, func() {
		writeTestLog(t, s, ownerID, logging.LevelWarn, logging.Event("stream_probe"), "pushed to the stream")
	})

	if !strings.Contains(body, "stream_probe") || !strings.Contains(body, "pushed to the stream") {
		t.Fatalf("stream body = %q", body)
	}
	if !strings.Contains(body, "data: ") || !strings.Contains(body, "id: ") {
		t.Fatalf("missing SSE framing: %q", body)
	}
	// Level has a pointer-receiver MarshalJSON; a value would encode as the
	// number 4 and the screen would render an empty badge.
	if !strings.Contains(body, `"level":"WARN"`) {
		t.Fatalf("level was not marshaled as a string: %q", body)
	}
}

func TestLogStreamDoesNotReplayHistory(t *testing.T) {
	s, ownerID, token := logStreamFixture(t)
	writeTestLog(t, s, ownerID, logging.LevelInfo, logging.Event("before_connect"), "old entry")
	ticket := issueStreamTicket(t, s, ownerID)

	body := runLogStream(t, s, token, "ticket="+ticket, func() {
		writeTestLog(t, s, ownerID, logging.LevelError, logging.Event("after_connect"), "new entry")
	})

	if strings.Contains(body, "before_connect") {
		t.Errorf("stream replayed history: %q", body)
	}
	if !strings.Contains(body, "after_connect") {
		t.Errorf("stream missed the new entry: %q", body)
	}
}

func TestLogStreamAppliesFilters(t *testing.T) {
	s, ownerID, token := logStreamFixture(t)
	// The cursor is the newest existing entry, so only entries written by the
	// callback are eligible.
	writeTestLog(t, s, ownerID, logging.LevelInfo, logging.Event("cursor"), "cursor")
	cursor, err := s.logStore.LatestID()
	if err != nil {
		t.Fatal(err)
	}
	ticket := issueStreamTicket(t, s, ownerID)

	body := runLogStream(t, s, token,
		"ticket="+ticket+"&after_id="+strconv.FormatInt(cursor, 10)+"&level=ERROR",
		func() {
			writeTestLog(t, s, ownerID, logging.LevelInfo, logging.Event("ignored_info"), "not an error")
			writeTestLog(t, s, ownerID, logging.LevelError, logging.Event("wanted_error"), "is an error")
		})

	if strings.Contains(body, "ignored_info") {
		t.Errorf("stream ignored the level filter: %q", body)
	}
	if !strings.Contains(body, "wanted_error") {
		t.Errorf("stream dropped the matching entry: %q", body)
	}
}
