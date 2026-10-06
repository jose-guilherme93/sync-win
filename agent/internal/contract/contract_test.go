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
	// Network rates are aggregated by the agent. If the dashboard ever sums
	// network_ifaces again it reintroduces the container double counting the
	// filter below exists to prevent.
	for _, field := range []string{"net_rx_rate", "net_tx_rate"} {
		if !containsField(c.Telemetry.Fields, field) {
			t.Errorf("telemetry.fields must declare %q", field)
		}
		if c.Telemetry.RateSemantics[field] == "" {
			t.Errorf("telemetry.rate_semantics must document %q", field)
		}
	}
	if len(c.Telemetry.NetworkInterfaceFilter.ExcludedPrefixes) == 0 {
		t.Fatal("telemetry.network_interface_filter.excluded_prefixes required")
	}
	for _, prefix := range []string{"lo", "veth", "br-", "docker"} {
		if !containsField(c.Telemetry.NetworkInterfaceFilter.ExcludedPrefixes, prefix) {
			t.Errorf("interface filter must exclude %q", prefix)
		}
	}
	if !containsField(c.Telemetry.NetworkInterfaceFilter.KeptExamples, "eth0") {
		t.Error("interface filter must document that real NICs are kept")
	}
	if !c.Telemetry.DiskPartitionDedup.ByDevice {
		t.Error("telemetry.disk_partition_dedup.by_device must be true")
	}
	if !containsField(c.Telemetry.DiskPartitionDedup.ExemptFilesystems, "btrfs") {
		t.Error("btrfs subvolumes must be exempt from the per-device dedup")
	}
	// The system inventory block exists so the agent has a bounded, declared
	// budget for the systemd and listening-port passes. Without these checks a
	// silent edit could uncap either collection.
	if c.SystemInventory.RefreshIntervalSeconds <= 0 {
		t.Error("system_inventory.refresh_interval_seconds must be positive")
	}
	if c.SystemInventory.Services.MaxUnits <= 0 || c.SystemInventory.Services.TimeoutSeconds <= 0 {
		t.Error("system_inventory.services limits incomplete")
	}
	if len(c.SystemInventory.Services.States) == 0 || len(c.SystemInventory.Services.EnabledStates) == 0 {
		t.Error("system_inventory.services must declare which states and enabled states it reports")
	}
	// The collector maps systemd onto exactly these buckets; a value the agent
	// cannot produce would filter every unit out at runtime.
	for _, status := range []string{"running", "failed", "stopped"} {
		if !containsField(c.SystemInventory.Services.States, status) {
			t.Errorf("system_inventory.services.states must include %q", status)
		}
	}
	if c.SystemInventory.Ports.MaxPorts <= 0 || c.SystemInventory.Ports.TimeoutSeconds <= 0 {
		t.Error("system_inventory.ports limits incomplete")
	}
	if len(c.SystemInventory.Ports.ListeningStates) == 0 {
		t.Error("system_inventory.ports.listening_states required")
	}
	for _, state := range []string{"LISTEN", "UNCONN"} {
		if !containsField(c.SystemInventory.Ports.ListeningStates, state) {
			t.Errorf("system_inventory.ports.listening_states must include %q", state)
		}
	}
	// The accessors fall back to their own defaults when the JSON is absent, so
	// they are asserted directly rather than through the loaded contract.
	empty := &Contract{}
	if empty.MaxServiceUnits() != 400 || empty.MaxOpenPorts() != 200 {
		t.Errorf("default inventory caps drifted: services=%d ports=%d", empty.MaxServiceUnits(), empty.MaxOpenPorts())
	}
	if empty.ServicesTimeout() != 10*time.Second || empty.PortsTimeout() != 10*time.Second {
		t.Error("default inventory timeouts drifted")
	}
	if c.MaxServiceUnits() != c.SystemInventory.Services.MaxUnits || c.MaxOpenPorts() != c.SystemInventory.Ports.MaxPorts {
		t.Error("accessors must report the values declared in the contract")
	}
}

func containsField(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
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
	if got := StatePath(); got != "/custom/state/sync-win/agent-state.json" {
		t.Fatalf("StatePath with XDG = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/tester")
	if got := StatePath(); got != "/home/tester/.local/state/sync-win/agent-state.json" {
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
			Transport                string   `json:"transport"`
			SupportedTypes           []string `json:"supported_types"`
			TimeoutSecondsDefault    int      `json:"timeout_seconds_default"`
			OutputCapBytes           int64    `json:"output_cap_bytes"`
			DockerLogsMaxTail        int      `json:"docker_logs_max_tail"`
			DockerExecTimeoutSeconds int      `json:"docker_exec_timeout_seconds"`
			PolicyPath               string   `json:"policy_path"`
			PolicyDefaults           struct {
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

func TestDockerExecTimeoutDefault(t *testing.T) {
	c := &Contract{}
	if got := c.DockerExecTimeout(); got != 30*time.Second {
		t.Fatalf("DockerExecTimeout() = %s, want 30s", got)
	}
	c.Commands.DockerExecTimeoutSeconds = 45
	if got := c.DockerExecTimeout(); got != 45*time.Second {
		t.Fatalf("DockerExecTimeout() = %s, want 45s", got)
	}
}
