package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lem/agent/collectors"
)

func TestSyncWorkspaceConfigsUploadsConfiguredProjectFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(project, ".vscode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".vscode", "settings.json"), []byte(`{"editor.tabSize": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices/device-1/sync-config":
			_ = json.NewEncoder(w).Encode(map[string]any{"extra_dirs": []string{}, "workspace_dirs": []string{project}})
		case "/api/devices/device-1/sync":
			var body struct {
				Preferences []preferencePayload `json:"preferences"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode sync request: %v", err)
			}
			if len(body.Preferences) != 1 || body.Preferences[0].Category != "workspace" {
				t.Errorf("unexpected workspace payload: %#v", body.Preferences)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"saved": body.Preferences})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := &agentState{LastWorkspaceHashes: map[string]string{}}
	if err := syncWorkspaceConfigs(server.URL, "device-1", "token", state); err != nil {
		t.Fatal(err)
	}
	if len(state.LastWorkspaceHashes) != 1 || state.LastWorkspaceSync.IsZero() {
		t.Fatalf("workspace state was not persisted: %#v", state)
	}
}

func TestScanWorkspaceCandidatesSkipsBinarySecretsAndUnchanged(t *testing.T) {
	home := t.TempDir()
	text := filepath.Join(home, "settings.json")
	binary := filepath.Join(home, "binary.json")
	secret := filepath.Join(home, "secret.json")
	if err := os.WriteFile(text, []byte(`{"editor.tabSize": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte{0, 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte(`{"github_token":"ghp_abcdefghijklmnopqrstuvwxyz012345"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := &agentState{LastWorkspaceHashes: map[string]string{}}
	candidates := []collectors.SaveCandidate{
		{Path: text, RelativePath: "project/.vscode/settings.json", SizeBytes: 22},
		{Path: binary, RelativePath: "project/.vscode/binary.json", SizeBytes: 2},
		{Path: secret, RelativePath: "project/.vscode/secret.json", SizeBytes: 60},
	}
	payloads, hashes := scanWorkspaceCandidates(candidates, state)
	if len(payloads) != 1 || len(hashes) != 1 || payloads[0].Category != "workspace" {
		t.Fatalf("unexpected workspace scan: payloads=%#v hashes=%#v", payloads, hashes)
	}
	state.LastWorkspaceHashes[payloads[0].RelativePath] = hashes[payloads[0].RelativePath]
	payloads, _ = scanWorkspaceCandidates(candidates[:1], state)
	if len(payloads) != 0 {
		t.Fatalf("unchanged workspace file was not skipped: %#v", payloads)
	}
}
