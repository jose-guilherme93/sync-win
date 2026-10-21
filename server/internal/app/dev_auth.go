package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"sync-win/server/internal/logging"
)

type devAuthRequest struct {
	Token string `json:"token"`
}

// handleAuthDev signs the dashboard into the configured development account
// with a fixed token, so the dev stack does not need a sign-in every restart.
// It is fail-closed: the flag is only set when SYNCWIN_ENV=development and
// SYNCWIN_DEV_TOKEN is present, and it never creates an account. The account
// is the same one SYNCWIN_ADMIN_EMAIL bootstraps (demo@sync-win.local by
// default in the dev compose file).
func (s *Server) handleAuthDev(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.flags.EnableDevLogin {
		s.writeError(w, http.StatusNotFound, errors.New("development sign-in is not enabled; set SYNCWIN_ENV=development and SYNCWIN_DEV_TOKEN"))
		return
	}
	if !s.allowRate(w, r, "auth-dev", 10, time.Minute) {
		return
	}
	var req devAuthRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	// Compare digests so the comparison does not depend on the token length.
	want := sha256.Sum256([]byte(s.flags.DevToken))
	got := sha256.Sum256([]byte(strings.TrimSpace(req.Token)))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid development token"))
		return
	}
	user, err := s.store.UserByEmail(s.flags.DevEmail)
	if err != nil {
		// Answer directly instead of through writeError: that helper redacts
		// every 5xx message, and this one names the exact fix a developer needs.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "dev account " + s.flags.DevEmail + " does not exist; run make dev-account or set SYNCWIN_ADMIN_EMAIL and SYNCWIN_ADMIN_PASSWORD",
		})
		return
	}
	s.log.Audit(logging.CatAuth, logging.EventAuthSuccess, "development session opened", map[string]any{
		"user_id": user.ID,
		"email":   user.Email,
	})
	s.writeAuth(w, r, user)
}
