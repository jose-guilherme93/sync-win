package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signingKeyPair generates a keypair and installs its public key as the agent's
// embedded update key for the duration of the test.
func signingKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	orig := agentUpdatePublicKey
	agentUpdatePublicKey = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { agentUpdatePublicKey = orig })
	return pub, priv
}

func TestUpdateAgentVerifiesSignatureAndReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "sync-win-agent")
	newBinary := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.7.0; fi\n")
	if err := os.WriteFile(current, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.6.1; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, priv := signingKeyPair(t)
	signature := ed25519.Sign(priv, newBinary)
	sum := sha256.Sum256(newBinary)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/version":
			_, _ = w.Write([]byte(`{"version":"0.7.0"}`))
		case "/api/agent/download":
			_, _ = w.Write(newBinary)
		case "/api/agent/signature":
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(signature)))
		case "/api/agent/checksums":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  sync-win-agent\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	mockBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(mockBin, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := updateAgentAt(current, server.URL, "sync-win-agent.service", false); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(current)
	if err != nil || string(updated) != string(newBinary) {
		t.Fatalf("binary was not replaced: %q err=%v", updated, err)
	}
}

func TestUpdateAgentRejectsBadSignature(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "sync-win-agent")
	original := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.6.1; fi\n")
	if err := os.WriteFile(current, original, 0o755); err != nil {
		t.Fatal(err)
	}
	signingKeyPair(t)
	newBinary := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.7.0; fi\n")
	wrongSig := ed25519.Sign(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), newBinary)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/version":
			_, _ = w.Write([]byte(`{"version":"0.7.0"}`))
		case "/api/agent/download":
			_, _ = w.Write(newBinary)
		case "/api/agent/signature":
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(wrongSig)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	mockBin := t.TempDir()
	_ = os.WriteFile(filepath.Join(mockBin, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := updateAgentAt(current, server.URL, "sync-win-agent.service", false); err == nil {
		t.Fatal("badly signed update must be rejected")
	}
	if updated, _ := os.ReadFile(current); string(updated) != string(original) {
		t.Fatal("binary was replaced despite a bad signature")
	}
}

func TestUpdateAgentRefusesWithoutSigningKey(t *testing.T) {
	orig := agentUpdatePublicKey
	agentUpdatePublicKey = ""
	defer func() { agentUpdatePublicKey = orig }()

	if err := verifyAgentSignature(filepath.Join(t.TempDir(), "missing"), []byte("sig")); err == nil {
		t.Fatal("expected refusal when no signing key is embedded")
	}
}

func TestNewerAgentVersion(t *testing.T) {
	cases := []struct {
		remote, current string
		want            bool
	}{
		{"0.6.2", "0.6.1", true},
		{"0.7.0", "0.6.9", true},
		{"0.6.1", "0.6.1", false},
		{"0.6.0", "0.6.1", false},
		{"v1.2.3", "1.2.2", true},
		{"0.6.2", "unknown", true},
	}
	for _, tc := range cases {
		got, err := newerAgentVersion(tc.remote, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("newerAgentVersion(%q, %q) = %v, %v; want %v", tc.remote, tc.current, got, err, tc.want)
		}
	}
}

func TestParseAgentVersionRejectsInvalid(t *testing.T) {
	for _, value := range []string{"", "1", "1.2.3.4", "1.x.3", "-1.0.0"} {
		if _, err := parseAgentVersion(value); err == nil {
			t.Errorf("parseAgentVersion(%q) accepted invalid value", value)
		}
	}
}

func TestMergeExecStartKeepsExistingCommand(t *testing.T) {
	current := "[Unit]\nDescription=SyncWin\n\n[Service]\nExecStart=/opt/custom/sync-win-agent daemon --server http://old:8080\nSupplementaryGroups=docker\n"
	desired := "[Unit]\nDescription=SyncWin\n\n[Service]\nExecStart=/usr/local/bin/sync-win-agent daemon --server http://new:8080\nSupplementaryGroups=docker systemd-journal\n"

	// The server renders its own ExecStart, which points at the public URL. A
	// device installed from a different URL must keep its own, or an update
	// silently repoints it.
	got := mergeExecStart(current, strings.Replace(desired,
		"ExecStart=/usr/local/bin/sync-win-agent daemon --server http://new:8080",
		"ExecStart={{SERVER_URL}}", 1))
	if !strings.Contains(got, "--server http://old:8080") {
		t.Errorf("existing ExecStart lost:\n%s", got)
	}
	if !strings.Contains(got, "systemd-journal") {
		t.Errorf("new group missing:\n%s", got)
	}
}

func TestMergeExecStartPrefersRenderedCommand(t *testing.T) {
	current := "[Service]\nExecStart=/opt/custom/agent\n"
	desired := "[Service]\nExecStart=/usr/local/bin/sync-win-agent daemon --server http://new:8080\nSupplementaryGroups=docker systemd-journal\n"
	got := mergeExecStart(current, desired)
	if !strings.Contains(got, "http://new:8080") {
		t.Errorf("a rendered ExecStart should win:\n%s", got)
	}
}

func TestFetchAgentUnitRejectsUnrenderedTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[Service]\nExecStart={{SERVER_URL}}\n"))
	}))
	defer srv.Close()

	// Writing this to /etc/systemd/system would break the service, so it must
	// be rejected rather than installed.
	if _, err := fetchAgentUnit(srv.URL, "dev-1", "tok"); err == nil {
		t.Fatal("expected an error for a unit still containing placeholders")
	}
}

func TestFetchAgentUnitRejectsNonUnitResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer srv.Close()

	if _, err := fetchAgentUnit(srv.URL, "dev-1", "tok"); err == nil {
		t.Fatal("expected an error for a response that is not a unit")
	}
}

func TestFetchAgentUnitSendsCredentials(t *testing.T) {
	var gotID, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.URL.Query().Get("device_id")
		gotToken = r.URL.Query().Get("device_token")
		_, _ = w.Write([]byte("[Unit]\n\n[Service]\nExecStart=/usr/local/bin/sync-win-agent\n"))
	}))
	defer srv.Close()

	unit, err := fetchAgentUnit(srv.URL, "dev-42", "secret token")
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "dev-42" {
		t.Errorf("device_id = %q", gotID)
	}
	if gotToken != "secret token" {
		t.Errorf("device_token = %q (must be escaped and round-trip)", gotToken)
	}
	if !strings.Contains(unit, "ExecStart=") {
		t.Errorf("unit = %q", unit)
	}
}
