package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func devLoginRequest(t *testing.T, s *Server, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAuthDev(rec, req)
	return rec
}

// The token is a development convenience, so it must not exist as a way in on
// a production server even when the variable is present.
func TestDevLoginIsEnabledOnlyInDevelopment(t *testing.T) {
	t.Setenv("SYNCWIN_DEV_TOKEN", "dev-token")
	t.Setenv("SYNCWIN_ADMIN_EMAIL", "dev@example.com")

	t.Setenv("SYNCWIN_ENV", "production")
	if flags := loadFeatureFlags(); flags.EnableDevLogin {
		t.Fatal("dev login enabled outside development")
	}

	t.Setenv("SYNCWIN_ENV", "development")
	flags := loadFeatureFlags()
	if !flags.EnableDevLogin {
		t.Fatal("dev login disabled in development")
	}
	if flags.DevEmail != "dev@example.com" || flags.DevToken != "dev-token" {
		t.Fatalf("flags = %+v", flags)
	}

	t.Setenv("SYNCWIN_DEV_TOKEN", "")
	if loadFeatureFlags().EnableDevLogin {
		t.Fatal("dev login enabled without a token")
	}
}

func TestDevLoginFailsClosedWhenDisabled(t *testing.T) {
	s := newTestServer(t)
	rec := devLoginRequest(t, s, "dev-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDevLoginRejectsAWrongToken(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableDevLogin = true
	s.flags.DevToken = "right-token"
	s.flags.DevEmail = "dev@example.com"

	rec := devLoginRequest(t, s, "wrong-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDevLoginOpensASessionForTheConfiguredAccount(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("dev@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	s.flags.EnableDevLogin = true
	s.flags.DevToken = "dev-token"
	s.flags.DevEmail = "dev@example.com"

	rec := devLoginRequest(t, s, "dev-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["owner_id"] != user.ID || body["email"] != "dev@example.com" {
		t.Fatalf("body = %v", body)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("no session cookie issued")
	}

	// The cookie the dashboard would now hold restores the same account.
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	s.handleAuthMe(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("auth/me status = %d, body = %s", meRec.Code, meRec.Body.String())
	}
}

// A dev token must not silently mint an account: the configured login has to
// exist, or the dashboard falls back to the sign-in screen with a clear reason.
func TestDevLoginDoesNotCreateAnAccount(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableDevLogin = true
	s.flags.DevToken = "dev-token"
	s.flags.DevEmail = "missing@example.com"

	rec := devLoginRequest(t, s, "dev-token")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dev account") {
		t.Fatalf("body does not explain the missing account: %s", rec.Body.String())
	}
	count, err := s.store.CountUsers()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("accounts = %d, want 0", count)
	}
}
