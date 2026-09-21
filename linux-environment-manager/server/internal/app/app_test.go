package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

	return &Server{store: st, logStore: logStore, log: structLogger}
}

func TestAgentInstallScriptRegistersAndVerifies(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh?owner_id=owner-x&hostname=target-pc&user=alice", nil)
	rec := httptest.NewRecorder()
	s.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "x-shellscript") {
		t.Fatalf("content type = %q", ct)
	}
	script := rec.Body.String()

	devices, err := s.store.ListDevicesForOwner("owner-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 registered device for owner-x, got %d", len(devices))
	}
	device := devices[0]
	if device.Hostname != "target-pc" || device.UserID != "alice" {
		t.Fatalf("unexpected device: %+v", device)
	}

	for _, want := range []string{
		"#!/usr/bin/env bash",
		`SERVER_URL="http://example.com"`,
		`DEVICE_ID="` + device.ID + `"`,
		`DEVICE_TOKEN="` + device.DeviceToken + `"`,
		"/api/agent/download",
		"lem-agent.service",
		"Restart=always",
		"StartLimitIntervalSec=0",
		"Verifying connection to the server",
		"/api/devices/$DEVICE_ID/heartbeat",
		"SUCCESS - LEM agent installed AND connected",
		"could NOT reach the server",
		"journalctl --user -u lem-agent",
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

func TestOwnerIDPriority(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/devices?owner_id=query-owner", nil)
	req.Header.Set("X-LEM-Anonymous-ID", "header-owner")
	if got := s.ownerID(req); got != "header-owner" {
		t.Fatalf("header must beat query param, got %q", got)
	}

	req.Header.Del("X-LEM-Anonymous-ID")
	if got := s.ownerID(req); got != "query-owner" {
		t.Fatalf("query param expected, got %q", got)
	}

	req.URL.RawQuery = ""
	if got := s.ownerID(req); got != "anonymous" {
		t.Fatalf("fallback expected anonymous, got %q", got)
	}
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
