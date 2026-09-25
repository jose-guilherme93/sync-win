package contract

import (
	"testing"
	"time"
)

func TestLoadEmbeddedContract(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("embedded contract must always load: %v", err)
	}
	if c.ContractVersion == "" || c.AgentVersion == "" {
		t.Fatal("contract/agent versions required")
	}
	if c.Collection.MaxFileBytes <= 0 || len(c.Collection.DefaultFiles) == 0 {
		t.Fatal("collection rules incomplete")
	}
	if !c.Collection.RejectedContentRules.BinaryOrInvalidUTF8 || len(c.Collection.RejectedContentRules.SecretPatterns) == 0 {
		t.Fatal("rejection rules incomplete")
	}
	if len(c.AppsInventory.Sources) == 0 || c.AppsInventory.RefreshIntervalSeconds <= 0 {
		t.Fatal("application inventory rules incomplete")
	}
	if len(c.Collection.Workspace.Files) == 0 || c.Collection.Workspace.MaxFileBytes <= 0 || c.Collection.Workspace.MaxTotalBytes <= 0 {
		t.Fatal("workspace configuration rules incomplete")
	}
	knownAppSources := map[string]bool{"apt": true, "flatpak": true, "pacman": true, "aur": true, "appimage": true}
	for _, source := range c.AppsInventory.Sources {
		if !knownAppSources[source] {
			t.Fatalf("unknown application inventory source: %s", source)
		}
	}
	for _, category := range c.Collection.Categories {
		if category != "kde" && category != "desktop" && category != "shell" && category != "app" && category != "general" && category != "workspace" && category != "saves" {
			t.Fatalf("unknown category in contract: %s", category)
		}
	}
	if c.Commands.Transport != "pull_only" {
		t.Fatalf("command transport must be pull_only, got %q", c.Commands.Transport)
	}
	if c.Commands.OutputCapBytes <= 0 || c.Commands.PolicyPath == "" || !c.Commands.PolicyDefaults.AllowDockerRead || c.Commands.PolicyDefaults.AllowDockerExec {
		t.Fatal("command limits or safe policy defaults incomplete")
	}
	if c.ServerLimitsMirrored.MaxFileBytesServer <= 0 || c.ServerLimitsMirrored.MaxRequestBytes <= 0 {
		t.Fatal("mirrored server limits incomplete")
	}
	if c.Resilience.HTTPTimeoutSeconds <= 0 || c.BackoffMax() <= time.Second {
		t.Fatal("resilience settings incomplete")
	}
}

func TestExpandPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	if got := ExpandPath("$HOME/.bashrc"); got != "/home/tester/.bashrc" {
		t.Fatalf("ExpandPath($HOME) = %q", got)
	}
	if got := ExpandPath("/abs/path"); got != "/abs/path" {
		t.Fatalf("absolute path changed: %q", got)
	}
}

func TestStatePathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	if got := StatePath(); got != "/custom/state/lem/agent-state.json" {
		t.Fatalf("StatePath with XDG = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/tester")
	if got := StatePath(); got != "/home/tester/.local/state/lem/agent-state.json" {
		t.Fatalf("StatePath default = %q", got)
	}
}

func TestDurationHelpers(t *testing.T) {
	c := &Contract{
		Resilience: struct {
			HTTPTimeoutSeconds    int     `json:"http_timeout_seconds"`
			BackoffBaseMultiplier int     `json:"backoff_base_multiplier"`
			BackoffMaxSeconds     int     `json:"backoff_max_seconds"`
			JitterFraction        float64 `json:"jitter_fraction"`
		}{HTTPTimeoutSeconds: 15, BackoffBaseMultiplier: 2, BackoffMaxSeconds: 600},
		Commands: struct {
			Transport             string   `json:"transport"`
			SupportedTypes        []string `json:"supported_types"`
			TimeoutSecondsDefault int      `json:"timeout_seconds_default"`
			OutputCapBytes        int64    `json:"output_cap_bytes"`
			PolicyPath            string   `json:"policy_path"`
			PolicyDefaults        struct {
				AllowInstallApp       bool `json:"allow_install_app"`
				AllowExcludeFile      bool `json:"allow_exclude_file"`
				AllowRestoreSaves     bool `json:"allow_restore_saves"`
				AllowLynisAudit       bool `json:"allow_lynis_audit"`
				AllowDockerRead       bool `json:"allow_docker_read"`
				AllowDockerLifecycle  bool `json:"allow_docker_lifecycle"`
				AllowDockerExec       bool `json:"allow_docker_exec"`
				AllowDockerPrune      bool `json:"allow_docker_prune"`
				AllowDockerCompose    bool `json:"allow_docker_compose"`
				CommandTimeoutSeconds int  `json:"command_timeout_seconds"`
			} `json:"policy_defaults"`
		}{TimeoutSecondsDefault: 900},
	}
	if c.HTTPTimeout() != 15*time.Second {
		t.Fatalf("HTTPTimeout = %s", c.HTTPTimeout())
	}
	if c.BackoffMax() != 10*time.Minute {
		t.Fatalf("BackoffMax = %s", c.BackoffMax())
	}
	if c.CommandTimeout(0) != 15*time.Minute {
		t.Fatalf("CommandTimeout(0) should use contract default, got %s", c.CommandTimeout(0))
	}
	if c.CommandTimeout(60) != time.Minute {
		t.Fatalf("CommandTimeout(60) = %s", c.CommandTimeout(60))
	}
}
