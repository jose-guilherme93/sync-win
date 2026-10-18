package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func notificationFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	t.Setenv("SYNCWIN_SECRET_KEY", "test-key-at-least-16-chars")
	s := newTestServer(t)
	user, err := s.store.CreateUser("notify@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, user.ID, session.Token
}

func notificationRequest(t *testing.T, s *Server, method, target, token, body string) *httptest.ResponseRecorder {
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
	path := target
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case strings.HasSuffix(path, "/notifications/events"):
		s.handleNotificationEvents(rec, req)
	case strings.HasSuffix(path, "/notifications/config"):
		s.handleNotificationConfig(rec, req)
	case strings.HasSuffix(path, "/notifications/inbox/read"):
		s.handleNotificationInboxRead(rec, req)
	case strings.HasSuffix(path, "/notifications/inbox"):
		s.handleNotificationInbox(rec, req)
	default:
		t.Fatalf("unhandled target %s", target)
	}
	return rec
}

func TestNotificationEventsCatalogRequiresSessionAndListsEveryType(t *testing.T) {
	s, _, token := notificationFixture(t)

	rec := notificationRequest(t, s, http.MethodGet, "/api/notifications/events", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", rec.Code)
	}

	rec = notificationRequest(t, s, http.MethodGet, "/api/notifications/events", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var catalog []struct {
		Type        string `json:"type"`
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 4 {
		t.Fatalf("catalog = %+v", catalog)
	}
	for _, entry := range catalog {
		if entry.Type == "" || entry.Label == "" || entry.Description == "" {
			t.Fatalf("incomplete catalog entry: %+v", entry)
		}
	}
}

func TestNotificationConfigDefaultsInAppEvents(t *testing.T) {
	s, _, token := notificationFixture(t)

	rec := notificationRequest(t, s, http.MethodGet, "/api/notifications/config", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var configs []notificationConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &configs); err != nil {
		t.Fatal(err)
	}
	var inbox *notificationConfigResponse
	for i := range configs {
		if configs[i].Provider == "web_inbox" {
			inbox = &configs[i]
		}
	}
	if inbox == nil {
		t.Fatal("web_inbox is missing from the default config")
	}
	if !inbox.Enabled || inbox.Configured {
		t.Fatalf("inbox defaults = %+v", inbox)
	}
	if len(inbox.Events) != 4 {
		t.Fatalf("inapp events = %v, want all four", inbox.Events)
	}
	var cfg map[string]any
	if err := json.Unmarshal(inbox.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["sound"] != "beep" || cfg["events_set"] != false {
		t.Fatalf("inbox config = %v", cfg)
	}
	if cfg["volume"] != 0.5 {
		t.Fatalf("volume = %v, want 0.5", cfg["volume"])
	}
}

func TestNotificationConfigSavesInAppPreferencesAndValidates(t *testing.T) {
	s, _, token := notificationFixture(t)

	body := `{"provider":"web_inbox","enabled":true,"events":["device_offline","sync_error"],"config":{"sound":"pulse","volume":0.8,"events_set":true}}`
	rec := notificationRequest(t, s, http.MethodPut, "/api/notifications/config", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = notificationRequest(t, s, http.MethodGet, "/api/notifications/config", token, "")
	var configs []notificationConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &configs); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range configs {
		if cfg.Provider != "web_inbox" {
			continue
		}
		if !cfg.Configured || len(cfg.Events) != 2 {
			t.Fatalf("saved inbox = %+v", cfg)
		}
		var config map[string]any
		if err := json.Unmarshal(cfg.Config, &config); err != nil {
			t.Fatal(err)
		}
		if config["sound"] != "pulse" || config["volume"] != 0.8 || config["events_set"] != true {
			t.Fatalf("saved config = %v", config)
		}
		return
	}
	t.Fatal("web_inbox not returned after save")
}

func TestNotificationConfigRejectsBadInAppSoundAndVolume(t *testing.T) {
	s, _, token := notificationFixture(t)

	for name, body := range map[string]string{
		"unknown sound": `{"provider":"web_inbox","enabled":true,"events":[],"config":{"sound":"loud","volume":0.5}}`,
		"volume high":   `{"provider":"web_inbox","enabled":true,"events":[],"config":{"sound":"beep","volume":2}}`,
		"volume low":    `{"provider":"web_inbox","enabled":true,"events":[],"config":{"sound":"beep","volume":-0.1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := notificationRequest(t, s, http.MethodPut, "/api/notifications/config", token, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNotificationInboxReadAll(t *testing.T) {
	s, owner, token := notificationFixture(t)
	for _, typ := range []string{"device_offline", "sync_error", "device_online"} {
		if _, err := s.store.InsertNotificationEvent(owner, typ, "dev-1", "host", "message"); err != nil {
			t.Fatal(err)
		}
	}

	rec := notificationRequest(t, s, http.MethodPost, "/api/notifications/inbox/read", token, `{"all":true}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = notificationRequest(t, s, http.MethodGet, "/api/notifications/inbox?limit=10", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox status = %d", rec.Code)
	}
	var events []struct {
		Read bool `json:"read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if !e.Read {
			t.Fatalf("event still unread: %+v", e)
		}
	}
}
