package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sync-win/server/internal/logging"
)

// Log streaming.
//
// The browser cannot send an Authorization header from EventSource and the
// session cookie alone is not enough for a long-lived GET, so the client first
// exchanges its session for a single-use ticket (the same store the
// notification stream uses). The stream then tails the logs table by id: the
// logger flushes in batches, so the server checks for new rows on a short
// interval and pushes the delta. Doing the read here, rather than publishing
// from the logger, keeps every filter in one place: the SQL that the page
// already uses.

// logParamsFromRequest maps the query string the viewer uses. It deliberately
// does not scope by owner: application logs are written by the server itself
// (startup, HTTP, agent, sync) with no owner, so an owner-scoped viewer shows
// nothing. Every authenticated account is an operator, as documented in
// SECURITY.md; the device journal log endpoint remains owner-scoped.
func logParamsFromRequest(r *http.Request) logging.QueryParams {
	q := r.URL.Query()
	params := logging.QueryParams{
		Level:         q.Get("level"),
		Category:      q.Get("category"),
		Event:         q.Get("event"),
		DeviceID:      q.Get("device_id"),
		UserID:        q.Get("user_id"),
		RequestID:     q.Get("request_id"),
		CorrelationID: q.Get("correlation_id"),
		From:          q.Get("from"),
		To:            q.Get("to"),
		Search:        q.Get("search"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params.Offset = n
		}
	}
	return params
}

func (s *Server) handleLogStreamToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if s.streamTickets == nil {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("stream tickets unavailable"))
		return
	}
	ticket, err := s.streamTickets.issue(ownerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ticket": ticket})
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID := s.ownerID(r)
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ownerID == "" || s.streamTickets == nil || !s.streamTickets.consume(ticket, ownerID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid stream ticket"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	// Filters come from the same query the page sends. Pagination does not
	// apply to a stream, so a client that reuses the page URL does not drag
	// limit/offset along.
	params := logParamsFromRequest(r)
	params.Limit = 0
	params.Offset = 0

	after := int64(0)
	for _, raw := range []string{r.URL.Query().Get("after_id"), r.Header.Get("Last-Event-ID")} {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			after = v
			break
		}
	}
	// No cursor means "from now": replaying the whole retention window into a
	// browser that already rendered the first page would duplicate history.
	if after == 0 {
		latest, err := s.logStore.LatestID()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		after = latest
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	interval := s.logStreamInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := r.Context()
	idle := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			entries, err := s.logStore.ChangesAfter(params, after, 200)
			if err != nil {
				// A failing read must not turn into a log storm on every tick:
				// the stream stays open and the next tick retries.
				idle++
			} else if len(entries) > 0 {
				for i := range entries {
					// LogEntry marshals Level through a pointer-receiver
					// MarshalJSON; passing a value here emitted the raw int
					// (level: 4) while /api/logs emitted "WARN".
					entry := &entries[i]
					data, marshalErr := json.Marshal(entry)
					if marshalErr != nil {
						continue
					}
					_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", entry.ID, data)
					after = entry.ID
				}
				flusher.Flush()
				idle = 0
				continue
			} else {
				idle++
			}

			// Comment lines keep proxies from closing an idle stream; the
			// event loop ignores them.
			if idle >= 15 {
				_, _ = fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
				idle = 0
			}
		}
	}
}
