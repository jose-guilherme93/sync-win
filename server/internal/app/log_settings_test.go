package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sync-win/server/internal/logging"
)

// logSettingsFixture returns a test server holding one session, with the
// retention cache primed the way Run() primes it. Without that priming the
// handler would report a zero-day window, which is only reachable in a test.
func logSettingsFixture(t *testing.T) (*Server, string) {
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
	s.logRetentionDays.Store(int32(logging.DefaultConfig().RetentionDays))
	return s, session.Token
}

func TestLogSettingsGetReportsEffectiveValues(t *testing.T) {
	s, token := logSettingsFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/log-settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleLogSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Level         string   `json:"level"`
		ConsoleLevel  string   `json:"console_level"`
		RetentionDays int      `json:"retention_days"`
		Levels        []string `json:"levels"`
		Categories    []string `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The reported level must be the level the logger will actually use.
	if body.Level != s.log.Level().String() {
		t.Errorf("level = %q, logger = %q", body.Level, s.log.Level().String())
	}
	if body.ConsoleLevel != s.log.ConsoleLevel().String() {
		t.Errorf("console_level = %q, logger = %q", body.ConsoleLevel, s.log.ConsoleLevel().String())
	}
	if body.RetentionDays != logging.DefaultConfig().RetentionDays {
		t.Errorf("retention_days = %d, want %d", body.RetentionDays, logging.DefaultConfig().RetentionDays)
	}
	// The pickers are populated from the server so they cannot drift from what
	// the logger accepts.
	if len(body.Levels) == 0 || len(body.Categories) == 0 {
		t.Fatalf("expected levels and categories, got %+v", body)
	}
	if !contains(body.Levels, "ERROR") {
		t.Errorf("levels missing ERROR: %v", body.Levels)
	}
	if !contains(body.Categories, "http") {
		t.Errorf("categories missing http: %v", body.Categories)
	}
}

func TestLogSettingsUpdateAppliesAndPersists(t *testing.T) {
	s, token := logSettingsFixture(t)

	req := httptest.NewRequest(http.MethodPut, "/api/log-settings",
		strings.NewReader(`{"level":"debug","console_level":"notice","retention_days":30}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleLogSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if s.log.Level() != logging.LevelDebug {
		t.Errorf("logger level = %s, want DEBUG", s.log.Level())
	}
	if s.log.ConsoleLevel() != logging.LevelNotice {
		t.Errorf("logger console level = %s, want NOTICE", s.log.ConsoleLevel())
	}
	if got := int(s.logRetentionDays.Load()); got != 30 {
		t.Errorf("retention cache = %d, want 30", got)
	}

	// The value must survive a restart, so it has to be on disk, not just in
	// memory.
	stored, found, err := s.logStore.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("settings were not persisted")
	}
	if stored.Level != "DEBUG" || stored.ConsoleLevel != "NOTICE" || stored.RetentionDays != 30 {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestLogSettingsRejectsInvalidValues(t *testing.T) {
	s, token := logSettingsFixture(t)
	before := s.log.Level()

	for name, body := range map[string]string{
		"unknown level":      `{"level":"LOUD","console_level":"WARN","retention_days":14}`,
		"audit is not a bar": `{"level":"AUDIT","console_level":"WARN","retention_days":14}`,
		"bad console level":  `{"level":"INFO","console_level":"LOUD","retention_days":14}`,
		"zero retention":     `{"level":"INFO","console_level":"WARN","retention_days":0}`,
		"huge retention":     `{"level":"INFO","console_level":"WARN","retention_days":99999}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/log-settings", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.handleLogSettings(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	// A rejected request must not have changed the running logger.
	if s.log.Level() != before {
		t.Fatalf("level changed after a rejected update: %s -> %s", before, s.log.Level())
	}
}

func TestLogSettingsRequiresSession(t *testing.T) {
	s, _ := logSettingsFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/log-settings", nil)
	rec := httptest.NewRecorder()
	s.handleLogSettings(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogSettingsRejectsUnsupportedMethods(t *testing.T) {
	s, token := logSettingsFixture(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/log-settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleLogSettings(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
