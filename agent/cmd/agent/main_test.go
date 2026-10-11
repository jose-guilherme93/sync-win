package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sync-win/agent/collectors"
)

func TestContractMatchesRuntime(t *testing.T) {
	c := syncwinContract
	if c.ContractVersion == "" || c.AgentVersion == "" {
		t.Fatalf("contract versions missing: %#v", c)
	}
	if agentVersion != c.AgentVersion || maxFileSizeBytes != c.Collection.MaxFileBytes {
		t.Fatal("runtime constants drifted from embedded contract")
	}
	if maxOutputBytes <= 0 || syncwinContract.HTTPTimeout() <= 0 || maxBackoff <= 0 {
		t.Fatalf("contract limits invalid: output=%d http=%s backoff=%s", maxOutputBytes, syncwinContract.HTTPTimeout(), maxBackoff)
	}
	if len(c.Collection.DefaultFiles) == 0 || !c.Collection.RejectedContentRules.BinaryOrInvalidUTF8 || len(c.Collection.RejectedContentRules.SecretPatterns) == 0 {
		t.Fatal("collection contract incomplete")
	}
}

func TestLocalPolicyControlsCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	policy := loadLocalPolicy()
	if policy.AllowInstallApp || policy.AllowExcludeFile || policy.AllowDockerExec || policy.AllowDockerPrune || policy.AllowRestartAgent || policy.AllowPackageUpdates || policy.AllowRebootDevice {
		t.Fatalf("defaults must be safe, got %#v", policy)
	}

	configDir := filepath.Join(home, ".config", "sync-win")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "policy.json"), []byte(`{"allow_install_app":false,"allow_exclude_file":true,"allow_restart_agent":true,"allow_package_updates":true,"allow_reboot_device":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy = loadLocalPolicy()
	if policy.AllowInstallApp {
		t.Fatal("policy must disable install_app")
	}
	if !policy.AllowRestartAgent || !policy.AllowPackageUpdates || !policy.AllowRebootDevice {
		t.Fatalf("policy action flags not loaded: %+v", policy)
	}

	if err := os.WriteFile(filepath.Join(configDir, "policy.json"), []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if policy = loadLocalPolicy(); policy.AllowInstallApp || !policy.AllowDockerRead {
		t.Fatal("malformed policy must fall back to safe defaults")
	}
}

func TestWebsocketBase(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8080":  "ws://localhost:8080",
		"https://x.example":      "wss://x.example",
		"http://localhost:8080/": "ws://localhost:8080",
		"wss://already":          "wss://already",
	}
	for in, want := range cases {
		if got := websocketBase(in); got != want {
			t.Fatalf("websocketBase(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSetCommandTogglesRemoteAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SYNCWIN_POLICY_PATH", filepath.Join(home, "etc-sync-win", "policy.json"))

	if policy := loadLocalPolicy(); policy.AllowRemoteAccess {
		t.Fatal("remote access must default to off")
	}

	if err := cmdSet([]string{"ssh", "on"}); err != nil {
		t.Fatalf("set ssh on: %v", err)
	}
	policy := loadLocalPolicy()
	if !policy.AllowRemoteAccess {
		t.Fatal("set ssh on did not enable remote access")
	}
	// Enabling one capability must not silently disable the others: a partial
	// policy file would make loadLocalPolicy authoritative over only its keys.
	if !policy.AllowDockerRead || !policy.AllowLynisAudit {
		t.Fatalf("set clobbered other policy fields: %+v", policy)
	}

	if err := cmdSet([]string{"ssh-user", "alice"}); err != nil {
		t.Fatalf("set ssh-user: %v", err)
	}
	if policy = loadLocalPolicy(); policy.SSHUser != "alice" {
		t.Fatalf("ssh-user not persisted: %+v", policy)
	}

	if err := cmdSet([]string{"ssh", "off"}); err != nil {
		t.Fatalf("set ssh off: %v", err)
	}
	policy = loadLocalPolicy()
	if policy.AllowRemoteAccess {
		t.Fatal("set ssh off did not disable remote access")
	}
	if policy.SSHUser != "alice" {
		t.Fatalf("ssh-user lost on toggle: %+v", policy)
	}

	if err := cmdSet([]string{"bogus", "on"}); err == nil {
		t.Fatal("unknown setting must fail")
	}
	if err := cmdSet([]string{"ssh", "maybe"}); err == nil {
		t.Fatal("invalid on/off must fail")
	}
	if err := cmdSet([]string{"ssh-user", "bad name"}); err == nil {
		t.Fatal("invalid user must fail")
	}
}

func TestDockerPolicyDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	policy := loadLocalPolicy()
	if !dockerRequestAllowed(policy, "list") {
		t.Fatal("docker read should be allowed by default")
	}
	for _, reqType := range []string{"start", "exec", "prune_system", "compose_write"} {
		if dockerRequestAllowed(policy, reqType) {
			t.Errorf("%s should be disabled by default", reqType)
		}
	}
}

func TestLynisAuditReportsMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	result := executeLynisAuditWithTimeout(time.Second)
	if result.Status != "failed" || !strings.Contains(result.Message, "Lynis is not installed") {
		t.Fatalf("missing Lynis result = %+v", result)
	}
}

func TestInstallAppRefusedAndSandboxed(t *testing.T) {
	if err := installApp("apt", "vim; rm -rf /", time.Second); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("unsafe package name must be refused, got %v", err)
	}
	if err := installApp("curl|bash", "vim", time.Second); err == nil || !strings.Contains(err.Error(), "unsupported app source") {
		t.Fatalf("unknown source must be refused, got %v", err)
	}
}

func TestInstallAppUsesFixedSourceCommand(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$INSTALL_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "flatpak"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("INSTALL_LOG", logPath)

	if err := installApp("flatpak", "org.example.App", time.Second); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "install\n--user\n--assumeyes\norg.example.App\n"; got != want {
		t.Fatalf("flatpak args = %q, want %q", got, want)
	}
	if err := installApp("appimage", "Example.AppImage", time.Second); err == nil || !strings.Contains(err.Error(), "local files") {
		t.Fatalf("AppImage install should be refused, got %v", err)
	}
}

func TestRunCommandLimits(t *testing.T) {
	output, err := runCommandWithLimits("sleep", []string{"5"}, 150*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("command must be killed on timeout, got %v (%q)", err, output)
	}

	output, err = runCommandWithLimits("sh", []string{"-c", "yes spam | head -c 200000; echo done"}, time.Second)
	if err != nil {
		t.Fatalf("normal command should pass: %v", err)
	}
	if int64(len(output)) > maxOutputBytes {
		t.Fatalf("output cap exceeded: %d > %d", len(output), maxOutputBytes)
	}
}

func TestFetchCollectionInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices/device-1/settings" {
			t.Errorf("settings path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"collection_interval_seconds":30}`))
	}))
	defer server.Close()

	got, err := fetchCollectionInterval(server.URL, "device-1", "device-token")
	if err != nil || got != 30*time.Second {
		t.Fatalf("interval = %s, err = %v", got, err)
	}

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"collection_interval_seconds":45}`))
	}))
	defer invalid.Close()
	if _, err := fetchCollectionInterval(invalid.URL, "device-1", "device-token"); err == nil {
		t.Fatal("unsupported interval should be rejected")
	}
}

func TestSendPendingUpdatesPostsExplicitStatusAndRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices/device-1/updates" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			Status  string `json:"status"`
			Updates []struct {
				Name string `json:"name"`
			} `json:"updates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Status != "ready" || len(payload.Updates) != 1 || payload.Updates[0].Name != "vim" {
			t.Errorf("payload = %+v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	report := collectors.UpdateInventory{
		Status:    "ready",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Updates:   []collectors.PendingUpdate{{Source: "apt", Name: "vim", CurrentVersion: "1", NewVersion: "2"}},
	}
	if err := sendPendingUpdates(server.URL, "device-1", "token", report); err != nil {
		t.Fatal(err)
	}
}

func TestPackageUpdatePlanIsFixedAndUsesInstalledManagers(t *testing.T) {
	installed := map[string]bool{"apt-get": true, "flatpak": true, "pacman": true, "yay": true}
	plan, err := packageUpdatePlan(func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"apt-get update",
		"apt-get upgrade -y",
		"flatpak update --user --noninteractive --assumeyes",
		"flatpak update --system --noninteractive --assumeyes",
		"yay -Syu --noconfirm",
	}
	if len(plan) != len(want) {
		t.Fatalf("update plan = %+v", plan)
	}
	for i, command := range plan {
		got := command.program + " " + strings.Join(command.args, " ")
		if got != want[i] {
			t.Errorf("plan[%d] = %q, want %q", i, got, want[i])
		}
	}
	if _, err := packageUpdatePlan(func(string) (string, error) { return "", os.ErrNotExist }); err == nil {
		t.Fatal("missing package managers should fail explicitly")
	}
}

func TestRemoteActionsFailClosedAndRebootUsesFixedArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("INVOCATION_ID", "")
	for _, action := range []string{"restart_agent", "update_packages", "reboot_device"} {
		result := executeCommand(agentCommand{Type: action}, "", "", "")
		if result.Status != "failed" || result.Message != "command disabled by local policy" {
			t.Errorf("default policy for %s = %+v", action, result)
		}
	}

	configDir := filepath.Join(home, ".config", "sync-win")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "policy.json"), []byte(`{"allow_reboot_device":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "shutdown-args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SHUTDOWN_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "shutdown"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sudoStub := "#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(sudoStub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("SHUTDOWN_LOG", logPath)
	result := executeCommand(agentCommand{Type: "reboot_device"}, "", "", "")
	if result.Status != "completed" || !strings.Contains(result.Message, "one minute") {
		t.Fatalf("reboot result = %+v", result)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "-r\n+1\n"; got != want {
		t.Fatalf("shutdown args = %q, want %q", got, want)
	}
}

func TestRestartAgentReportsBeforeExitingForSystemd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("INVOCATION_ID", "test-invocation")
	configDir := filepath.Join(home, ".config", "sync-win")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "policy.json"), []byte(`{"allow_restart_agent":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resultPosted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id":"cmd-1","type":"restart_agent"}]`))
			return
		}
		var result map[string]string
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			t.Error(err)
		}
		resultPosted = result["status"] == "completed"
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := processCommands(server.URL, "device-1", "device-token")
	if err != errAgentRestartRequested || !resultPosted {
		t.Fatalf("processCommands err=%v, resultPosted=%v", err, resultPosted)
	}
}

func TestExcludeFileIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for range 3 {
		if err := excludeFile(".bashrc"); err != nil {
			t.Fatalf("excludeFile: %v", err)
		}
	}
	data, err := os.ReadFile(exclusionsPath())
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), ".bashrc"); lines != 1 {
		t.Fatalf("excludeFile appended duplicates: %d entries", lines)
	}
	if err := excludeFile("../../etc/passwd"); err == nil {
		t.Fatal("traversal path must be refused")
	}
	if err := excludeFile("/abs/path"); err == nil {
		t.Fatal("absolute path must be refused")
	}
}

func TestStatePruneHashes(t *testing.T) {
	state := &agentState{LastSyncHashes: map[string]string{
		".bashrc":            "a",
		".config/kdeglobals": "b",
		"deleted-file.conf":  "c",
	}}
	state.pruneSyncHashes(map[string]bool{".bashrc": true, ".config/kdeglobals": true})
	if _, ok := state.LastSyncHashes["deleted-file.conf"]; ok {
		t.Fatal("stale hash was not pruned")
	}
	if len(state.LastSyncHashes) != 2 {
		t.Fatalf("live hashes lost: %#v", state.LastSyncHashes)
	}
}

func TestCategorize(t *testing.T) {
	home := "/home/tester"
	cases := []struct {
		path string
		want string
	}{
		{filepath.Join(home, ".config", "kdeglobals"), "kde"},
		{filepath.Join(home, ".kde", "share", "config", "kcminputrc"), "kde"},
		{filepath.Join(home, ".config", "gtk-3.0", "settings.ini"), "desktop"},
		{filepath.Join(home, ".config", "gtk-4.0", "settings.ini"), "desktop"},
		{filepath.Join(home, ".bashrc"), "shell"},
		{filepath.Join(home, ".zshrc"), "shell"},
		{filepath.Join(home, ".profile"), "shell"},
		{filepath.Join(home, ".config", "Code", "User", "settings.json"), "app"},
		{filepath.Join(home, "notes.txt"), "general"},
	}
	for _, tc := range cases {
		if got := categorizePath(tc.path); got != tc.want {
			t.Errorf("categorizePath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestRelativeToHome(t *testing.T) {
	home := "/home/tester"
	cases := []struct {
		path string
		want string
	}{
		{filepath.Join(home, ".bashrc"), ".bashrc"},
		{filepath.Join(home, ".config", "gtk-3.0", "settings.ini"), ".config/gtk-3.0/settings.ini"},
		{"/etc/passwd", "passwd"},
	}
	for _, tc := range cases {
		if got := relativeToHome(home, tc.path); got != tc.want {
			t.Errorf("relativeToHome(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFindSecret(t *testing.T) {
	safe := []string{
		"[General]\nColorScheme=BreezyDark\n",
		"export EDITOR=vim\n",
		"{ \"editor.fontSize\": 14 }\n",
	}
	for _, content := range safe {
		if reason := findSecret(content); reason != "" {
			t.Errorf("findSecret flagged safe content: %q (%s)", content, reason)
		}
	}
	secrets := []string{
		"aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
		"github_token=ghp_abcdefghijklmnopqrstuvwxyz012345",
		"slack=xoxb-123456789012-abcdefghijklmn",
		"stripe_key=sk_live_abcdefghij12345",
		"google_api_key_here_is_long_enough33=AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----",
	}
	for _, content := range secrets {
		if reason := findSecret(content); reason == "" {
			t.Errorf("findSecret missed secret: %q", content)
		}
	}
}

func TestBuildPreferenceRejectsUnsafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}

	bigPath := filepath.Join(home, "big.conf")
	if err := os.WriteFile(bigPath, []byte(strings.Repeat("a", int(maxFileSizeBytes)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPreference(home, bigPath, ""); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized file must be rejected, got %v", err)
	}

	binaryPath := filepath.Join(home, "blob.bin")
	if err := os.WriteFile(binaryPath, append([]byte("text"), 0, 1, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPreference(home, binaryPath, ""); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary file must be rejected, got %v", err)
	}

	secretPath := filepath.Join(home, "leak.env")
	if err := os.WriteFile(secretPath, []byte("token=ghp_abcdefghijklmnopqrstuvwxyz012345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPreference(home, secretPath, ""); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret-bearing file must be rejected, got %v", err)
	}
}

func TestCollectPreferencesSkipsUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	configDir := filepath.Join(home, ".config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "kdeglobals")
	if err := os.WriteFile(path, []byte("[General]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	state := &agentState{LastSyncHashes: map[string]string{}}
	payloads := collectPreferences([]string{path}, state)
	if len(payloads) != 1 {
		t.Fatalf("expected first collection to include file, got %d", len(payloads))
	}
	relativePath := payloads[0].RelativePath
	state.LastSyncHashes[relativePath] = hashContent(payloads[0].Content)
	state.LastPreferenceSync = time.Now()
	state.save()

	if payloads := collectPreferences([]string{path}, state); len(payloads) != 0 {
		t.Fatalf("unchanged file should be skipped, got %d payloads", len(payloads))
	}

	reloaded := loadAgentState()
	if reloaded.LastSyncHashes[relativePath] == "" || reloaded.LastPreferenceSync.IsZero() {
		t.Fatalf("state did not persist across reload: %#v", reloaded)
	}

	if err := os.WriteFile(path, []byte("[General]\nColorScheme=Dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if payloads = collectPreferences([]string{path}, state); len(payloads) != 1 {
		t.Fatalf("changed file should be collected again, got %d payloads", len(payloads))
	}
}

func TestSyncPreferencesRetriesUntilSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export EDITOR=vi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	requests := 0
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices/device-1/sync" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		requests++
		if fail {
			http.Error(w, "temporary outage", http.StatusInternalServerError)
			return
		}
		var body struct {
			DeviceToken string              `json:"device_token"`
			Preferences []preferencePayload `json:"preferences"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.DeviceToken != "device-token" || len(body.Preferences) != 1 {
			t.Errorf("unexpected sync request: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"saved": body.Preferences})
	}))
	defer server.Close()

	state := &agentState{LastSyncHashes: map[string]string{}}
	if err := syncPreferences(server.URL, "device-1", "device-token", state); err == nil {
		t.Fatal("failed upload should return an error")
	}
	if len(state.LastSyncHashes) != 0 || !state.LastPreferenceSync.IsZero() {
		t.Fatalf("failed upload advanced state: %#v", state)
	}

	fail = false
	if err := syncPreferences(server.URL, "device-1", "device-token", state); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if requests != 2 || state.LastSyncHashes[".bashrc"] == "" || state.LastPreferenceSync.IsZero() {
		t.Fatalf("successful retry did not persist state: requests=%d state=%#v", requests, state)
	}
	if err := syncPreferences(server.URL, "device-1", "device-token", state); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("unchanged preference was uploaded again: requests=%d", requests)
	}
}

func TestSyncPreferencesDoesNotHashRejectedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export EDITOR=vi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"saved":    []preferencePayload{},
			"rejected": []map[string]string{{"filename": ".bashrc", "reason": "test rejection"}},
		})
	}))
	defer server.Close()

	state := &agentState{LastSyncHashes: map[string]string{}}
	if err := syncPreferences(server.URL, "device-1", "device-token", state); err != nil {
		t.Fatal(err)
	}
	if state.LastSyncHashes[".bashrc"] != "" {
		t.Fatal("rejected preference must remain eligible for retry")
	}
}

func TestPreferencePathsHonorsExtraAllowlist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	allowDir := filepath.Join(home, ".config", "sync-win")
	if err := os.MkdirAll(allowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(home, "custom-pref.conf")
	if err := os.WriteFile(allowed, []byte("key=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	allowlist := filepath.Join(allowDir, "allowed-files")
	if err := os.WriteFile(allowlist, []byte("# comment\n\ncustom-pref.conf\n/etc/passwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := preferencePaths()
	found := false
	for _, path := range paths {
		if path == allowed {
			found = true
		}
		if path == "/etc/passwd" {
			t.Fatal("outside-home allowlist entry was accepted")
		}
	}
	if !found {
		t.Fatalf("explicit allowlist file was not included: %v", paths)
	}
}

func TestClampDockerLogTail(t *testing.T) {
	max := syncwinContract.DockerLogsMaxTail()
	if max <= 0 {
		t.Fatal("contract must define a positive docker logs max tail")
	}
	if got := clampDockerLogTail(max + 1000); got != max {
		t.Fatalf("clamp = %d, want %d", got, max)
	}
	if got := clampDockerLogTail(50); got != 50 {
		t.Fatalf("clamp(50) = %d, want 50", got)
	}
	if got := clampDockerLogTail(0); got != 200 {
		t.Fatalf("clamp(0) = %d, want 200", got)
	}
}

func TestPreferencePathsHonorsExcludeList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	allowDir := filepath.Join(home, ".config", "sync-win")
	if err := os.MkdirAll(allowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(home, "custom-pref.conf")
	if err := os.WriteFile(allowed, []byte("key=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allowDir, "allowed-files"), []byte("custom-pref.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Seed the excluded list directly: the file must not be collected.
	if err := os.WriteFile(filepath.Join(allowDir, "excluded-files"), []byte("custom-pref.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range preferencePaths() {
		if path == allowed {
			t.Fatal("excluded file was still collected")
		}
	}
	// And the exclude_file command must persist to the same list read above.
	if err := os.Remove(filepath.Join(allowDir, "excluded-files")); err != nil {
		t.Fatal(err)
	}
	if err := excludeFile("custom-pref.conf"); err != nil {
		t.Fatal(err)
	}
	for _, path := range preferencePaths() {
		if path == allowed {
			t.Fatal("exclude_file did not filter collection")
		}
	}
}

func TestSendAppInventorySendsEmptyArray(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Apps json.RawMessage `json:"apps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if string(body.Apps) != "[]" {
			t.Errorf("apps = %s, want []", body.Apps)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := sendAppInventory(server.URL, "device-1", "device-token", &agentState{LastSyncHashes: map[string]string{}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSendAppInventoryRetriesUntilSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	inventory := []collectors.AppInfo{{
		Source:  "flatpak",
		Name:    "org.mozilla.firefox",
		Version: "141.0",
	}}
	requests := 0
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/devices/device-1/apps" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer device-token" {
			t.Errorf("authorization = %q", got)
		}
		var body struct {
			DeviceToken string               `json:"device_token"`
			Apps        []collectors.AppInfo `json:"apps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.DeviceToken != "device-token" || len(body.Apps) != 1 || body.Apps[0].Name != inventory[0].Name {
			t.Errorf("unexpected inventory request: %+v", body)
		}
		requests++
		if fail {
			http.Error(w, "temporary outage", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	state := &agentState{LastSyncHashes: map[string]string{}}
	if err := sendAppInventory(server.URL, "device-1", "device-token", state, inventory); err == nil {
		t.Fatal("failed inventory upload should return an error")
	}
	if !state.LastAppInventorySync.IsZero() {
		t.Fatal("failed inventory upload advanced state")
	}

	fail = false
	if err := sendAppInventory(server.URL, "device-1", "device-token", state, inventory); err != nil {
		t.Fatalf("inventory retry failed: %v", err)
	}
	if requests != 2 || state.LastAppInventorySync.IsZero() {
		t.Fatalf("successful retry did not persist state: requests=%d state=%#v", requests, state)
	}
	if reloaded := loadAgentState(); reloaded.LastAppInventorySync.IsZero() {
		t.Fatal("inventory timestamp was not persisted")
	}
}

func TestSystemMetadataCollection(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("LANG", "pt_BR.UTF-8")
	if got := detectDesktopEnvironment(); got != "kde" {
		t.Fatalf("detectDesktopEnvironment = %q, want kde", got)
	}
	if got := readLocale(); got != "pt_BR" {
		t.Fatalf("readLocale = %q, want pt_BR", got)
	}

	t.Setenv("XDG_CURRENT_DESKTOP", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "kdeglobals"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectDesktopEnvironment(); got != "kde" {
		t.Fatalf("fallback should detect KDE via kdeglobals, got %q", got)
	}

	stats, _, err := collectHardwareStats(&hardwareSample{}, time.Now())
	if err != nil {
		t.Fatalf("collectHardwareStats: %v", err)
	}
	if stats.AgentVersion != agentVersion || stats.Architecture == "" || stats.DesktopEnvironment != "kde" {
		t.Fatalf("system metadata incomplete: %#v", stats)
	}
}

func TestBuildPreferenceAllowsSmallText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".config", "gtk-3.0")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "settings.ini")
	content := "[Settings]\ngtk-theme=Adwaita-dark\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := buildPreference(home, path, "")
	if err != nil {
		t.Fatalf("build preference: %v", err)
	}
	if payload.Content != content {
		t.Fatalf("content mismatch: %q", payload.Content)
	}
	if payload.Category != "desktop" {
		t.Fatalf("expected auto category desktop, got %q", payload.Category)
	}
	if payload.RelativePath != ".config/gtk-3.0/settings.ini" {
		t.Fatalf("unexpected relative path %q", payload.RelativePath)
	}
	if payload.Filename != "settings.ini" {
		t.Fatalf("unexpected filename %q", payload.Filename)
	}

	payload, err = buildPreference(home, path, "custom")
	if err != nil {
		t.Fatalf("build preference with override: %v", err)
	}
	if payload.Category != "custom" {
		t.Fatalf("category override ignored, got %q", payload.Category)
	}
}

func TestSensitivePreferencePathUsesContractGlobs(t *testing.T) {
	rejected := []string{"id_rsa_backup", "server.pem", "cert.key", "store.p12", "bundle.pfx", ".netrc"}
	for _, name := range rejected {
		if !sensitivePreferencePath(filepath.Join("/home/u", name)) {
			t.Errorf("sensitivePreferencePath(%q) = false, want true", name)
		}
	}
	if sensitivePreferencePath("/home/u/.config/foo.conf") {
		t.Error("ordinary config file was flagged as sensitive")
	}
}

func TestParseDiskIODedupsPartitions(t *testing.T) {
	data := strings.Join([]string{
		"   8       0 sda 10 0 1000 0 20 0 2000 0 0 0 0",
		"   8       1 sda1 10 0 1000 0 20 0 2000 0 0 0 0",
		" 259       0 nvme0n1 10 0 4000 0 20 0 8000 0 0 0 0",
		" 259       1 nvme0n1p1 10 0 4000 0 20 0 8000 0 0 0 0",
		"   7       0 loop0 10 0 800 0 20 0 800 0 0 0 0",
	}, "\n")
	whole := map[string]bool{"sda": true, "nvme0n1": true}
	read, write := parseDiskIO(data, func(name string) bool { return whole[name] })
	if want := uint64((1000 + 4000) * 512); read != want {
		t.Fatalf("read = %d, want %d", read, want)
	}
	if want := uint64((2000 + 8000) * 512); write != want {
		t.Fatalf("write = %d, want %d", write, want)
	}
}
