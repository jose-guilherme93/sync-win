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
	if _, err := fetchAgentUnit(srv.URL); err == nil {
		t.Fatal("expected an error for a unit still containing placeholders")
	}
}

func TestFetchAgentUnitRejectsNonUnitResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer srv.Close()

	if _, err := fetchAgentUnit(srv.URL); err == nil {
		t.Fatal("expected an error for a response that is not a unit")
	}
}

// The unit refresh must work from a device whose update service carries no
// credentials, which is every device installed before those flags existed. That
// is exactly the population that needs its unit repaired.
func TestFetchAgentUnitNeedsNoCredentials(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.URL.Path != "/api/agent/units" {
			t.Errorf("path = %q, want /api/agent/units (the public endpoint)", r.URL.Path)
		}
		_, _ = w.Write([]byte("[Unit]\n\n[Service]\nExecStart=/usr/local/bin/sync-win-agent\n"))
	}))
	defer srv.Close()

	unit, err := fetchAgentUnit(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "device_token") {
		t.Errorf("query %q carries a credential; it must not be required", query)
	}
	if !strings.Contains(query, "name=agent") {
		t.Errorf("query = %q, want name=agent", query)
	}
	if !strings.Contains(unit, "ExecStart=") {
		t.Errorf("unit = %q", unit)
	}
}

func TestApplySupplementaryGroups(t *testing.T) {
	unit := "[Unit]\nDescription=x\n\n[Service]\nExecStart=/bin/true\nSupplementaryGroups=docker systemd-journal\n"

	got := applySupplementaryGroups(unit, "systemd-journal")
	if !strings.Contains(got, "SupplementaryGroups=systemd-journal") {
		t.Errorf("did not replace the line:\n%s", got)
	}
	if strings.Contains(got, "docker") {
		t.Errorf("kept a group the host does not have:\n%s", got)
	}

	// No groups available: the line must go, not be left empty.
	got = applySupplementaryGroups(unit, "")
	if strings.Contains(got, "SupplementaryGroups") {
		t.Errorf("kept an empty directive, which systemd rejects:\n%s", got)
	}

	// A unit without the line gains it under [Service].
	plain := "[Unit]\nDescription=x\n\n[Service]\nExecStart=/bin/true\n"
	got = applySupplementaryGroups(plain, "adm")
	if !strings.Contains(got, "SupplementaryGroups=adm") {
		t.Errorf("did not add the line:\n%s", got)
	}
}

func TestWriteUpdateRequestTwiceDiffers(t *testing.T) {
	// The path unit fires on PathChanged. Identical consecutive writes would
	// leave some versions of systemd unmoved, so the body must change.
	path := filepath.Join(t.TempDir(), "nested", "update-request")

	read := func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	if err := writeUpdateRequestTo(path, "0.6.3"); err != nil {
		t.Fatal(err)
	}
	first := read()
	if err := writeUpdateRequestTo(path, "0.6.3"); err != nil {
		t.Fatal(err)
	}
	second := read()

	if first == second {
		t.Errorf("consecutive requests are byte-identical: %q", first)
	}
	if !strings.Contains(second, "0.6.3") {
		t.Errorf("request body %q should name the target version", second)
	}
	// The parent directory is created, since the state dir may not exist yet on
	// a fresh install.
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent directory was not created: %v", err)
	}
}

// The condition that emptied the fleet's Logs screen: the agent was up to date,
// so the updater returned early and never reconciled the unit.
func TestReconcileUnitIsReachedWhenAgentIsCurrent(t *testing.T) {
	var unitCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unitCalls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// A current agent still reaches the unit refresh. Before this, the current
	// path returned early and the unit was never reconciled at all.
	err := reconcileUnit(srv.URL, "sync-win-agent.service", false, "dev-1", "tok")
	if unitCalls == 0 {
		t.Fatal("the unit endpoint was never called; the refresh was skipped")
	}
	if err == nil {
		t.Error("a 500 from the unit endpoint must be reported, not silently ignored")
	}
}

func TestReconcileUnitToleratesOlderServerWithoutTheEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// An older server must not fail an update just because it cannot serve the
	// unit. This is the one case that is deliberately not an error.
	if err := reconcileUnit(srv.URL, "sync-win-agent.service", false, "dev-1", "tok"); err != nil {
		t.Errorf("a missing unit endpoint should be tolerated, got %v", err)
	}
}

func TestNewerAgentVersionIsFalseForEqualVersions(t *testing.T) {
	newer, err := newerAgentVersion("0.6.3", "0.6.3")
	if err != nil {
		t.Fatal(err)
	}
	if newer {
		t.Fatal("an identical version must not be considered newer")
	}
}

func TestJournalAccessDeniedDetectsPermissionFailures(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{`journalctl: exit status 1: Failed to add match: Permission denied`, true},
		{`journalctl: Access denied`, true},
		{`journalctl: not authorized`, true},
		{`journalctl: exit status 1`, false},
		{``, false},
		{`journalctl returned no output`, false},
	} {
		if got := journalAccessDenied(tc.status); got != tc.want {
			t.Errorf("journalAccessDenied(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
