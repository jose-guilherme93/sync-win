package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lem/server/internal/logging"
	"lem/server/internal/store"
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

	return &Server{store: st, logStore: logStore, log: structLogger, streamTickets: newStreamTicketStore(), rateLimiter: newRateLimiter()}
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
		"lem-agent.service",
		"Restart=always",
		"StartLimitIntervalSec=0",
		"Verifying connection to the server",
		"/api/devices/$DEVICE_ID/heartbeat",
		"SUCCESS - LEM agent installed AND connected",
		"could NOT reach the server",
		"journalctl --user -u lem-agent",
		"lem-agent-update.timer",
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
	req.Header.Set("X-LEM-Anonymous-ID", "header-owner")
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
	t.Setenv("LEM_ENABLE_FINGERPRINT_RECONNECT", "")
	t.Setenv("LEM_ENABLE_LEGACY_INSTALL", "")
	t.Setenv("LEM_ENABLE_REMOTE_MUTATIONS", "")
	t.Setenv("LEM_ENABLE_DOCKER_MUTATIONS", "")

	flags := loadFeatureFlags()
	if flags.EnableFingerprintReconnect || flags.EnableLegacyInstall || flags.EnableRemoteMutations || flags.EnableDockerMutations {
		t.Fatalf("security feature flags must default to false: %#v", flags)
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
	t.Setenv("LEM_CORS_ALLOWED_ORIGIN", "")
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
	t.Setenv("LEM_CORS_ALLOWED_ORIGIN", "http://localhost:5173, http://127.0.0.1:5173")

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
	t.Setenv("LEM_CORS_ALLOWED_ORIGIN", "http://localhost:5173")
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
		t.Setenv("LEM_CORS_ALLOWED_ORIGIN", value)
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
