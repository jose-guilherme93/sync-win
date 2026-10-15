package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"sync-win/server/internal/logging"
)

// logSettingsResponse is what the settings screen renders: the current values
// plus the enumerations it must offer. Shipping the levels and categories from
// the server keeps the pickers in step with what the logger actually accepts,
// instead of duplicating the lists in TypeScript where they would drift.
type logSettingsResponse struct {
	Level         string   `json:"level"`
	ConsoleLevel  string   `json:"console_level"`
	RetentionDays int      `json:"retention_days"`
	Levels        []string `json:"levels"`
	Categories    []string `json:"categories"`
}

// handleLogSettings reads and updates the application-wide logging settings.
//
// The values are global rather than per-owner on purpose: the server runs a
// single logger against a single logs table, so a per-owner level would apply
// to every account's entries while appearing to be scoped to one.
func (s *Server) handleLogSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(logSettingsResponse{
			Level:         s.log.Level().String(),
			ConsoleLevel:  s.log.ConsoleLevel().String(),
			RetentionDays: int(s.logRetentionDays.Load()),
			Levels:        logging.SettableLevels(),
			Categories:    logging.Categories(),
		})

	case http.MethodPut:
		var req logging.Settings
		if !s.decodeBody(w, r, &req) {
			return
		}
		req.Level = strings.ToUpper(strings.TrimSpace(req.Level))
		req.ConsoleLevel = strings.ToUpper(strings.TrimSpace(req.ConsoleLevel))
		if err := req.Validate(); err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}

		// Apply to the running logger first, then persist. A failed save leaves
		// the logger changed for this process but reports the error, which is
		// the honest outcome: the operator sees the failure instead of a
		// setting that silently did not stick.
		level, _ := logging.ParseLevelStrict(req.Level)
		console, _ := logging.ParseLevelStrict(req.ConsoleLevel)
		s.log.SetLevel(level)
		s.log.SetConsoleLevel(console)
		s.logRetentionDays.Store(int32(req.RetentionDays))
		if err := s.logStore.SaveSettings(req); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}

		s.log.Audit(logging.CatSystem, logging.EventAppStarted, "log settings updated", map[string]any{
			"level":          req.Level,
			"console_level":  req.ConsoleLevel,
			"retention_days": req.RetentionDays,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(req)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
