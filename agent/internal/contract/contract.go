// Package contract embeds the collection contract that defines everything
// the agent collects and how it talks to the server. The JSON file is the
// single source of truth; runtime constants must match it (enforced by tests).
package contract

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed contract.json
var raw []byte

type Contract struct {
	ContractVersion string `json:"contract_version"`
	AgentVersion    string `json:"agent_version"`
	Collection      struct {
		MaxFileBytes         int64    `json:"max_file_bytes"`
		DefaultFiles         []string `json:"default_preference_files"`
		ExtraAllowlistPath   string   `json:"extra_allowlist_path"`
		ExcludedFilesPath    string   `json:"excluded_files_path"`
		Categories           []string `json:"categories"`
		RejectedContentRules struct {
			BinaryOrInvalidUTF8 bool     `json:"binary_or_invalid_utf8"`
			EmptyContent        bool     `json:"empty_content"`
			SecretPatterns      []string `json:"secret_patterns"`
			SensitiveFilenames  []string `json:"sensitive_filenames"`
		} `json:"rejected_content_rules"`
		Saves struct {
			Roots                 []string `json:"roots"`
			IncludeExtensions     []string `json:"include_extensions"`
			ExcludedDirs          []string `json:"excluded_dirs"`
			ExcludedExtensions    []string `json:"excluded_extensions"`
			MaxSaveFileBytes      int64    `json:"max_save_file_bytes"`
			MaxTotalBytesPerCycle int64    `json:"max_total_bytes_per_cycle"`
			MaxDepth              int      `json:"max_depth"`
		} `json:"saves"`
		Workspace struct {
			Files         []string `json:"files"`
			MaxFileBytes  int64    `json:"max_file_bytes"`
			MaxTotalBytes int64    `json:"max_total_bytes"`
		} `json:"workspace"`
	} `json:"collection"`
	Telemetry struct {
		IntervalSecondsDefault int               `json:"interval_seconds_default"`
		IntervalSecondsAllowed []int             `json:"interval_seconds_allowed"`
		Fields                 []string          `json:"fields"`
		RateSemantics          map[string]string `json:"rate_semantics"`
		NetworkInterfaceFilter struct {
			ExcludedPrefixes []string `json:"excluded_prefixes"`
			Rationale        string   `json:"rationale"`
			KeptExamples     []string `json:"kept_examples"`
		} `json:"network_interface_filter"`
		DiskPartitionDedup struct {
			ByDevice          bool     `json:"by_device"`
			ExemptFilesystems []string `json:"exempt_filesystems"`
			Rationale         string   `json:"rationale"`
		} `json:"disk_partition_dedup"`
	} `json:"telemetry"`
	AppsInventory struct {
		Sources                []string `json:"sources"`
		RefreshIntervalSeconds int      `json:"refresh_interval_seconds"`
		PendingUpdates         struct {
			RefreshIntervalSeconds int   `json:"refresh_interval_seconds"`
			TimeoutSeconds         int   `json:"timeout_seconds"`
			MaxOutputBytes         int64 `json:"max_output_bytes"`
		} `json:"pending_updates"`
	} `json:"apps_inventory"`
	SystemInventory struct {
		RefreshIntervalSeconds int `json:"refresh_interval_seconds"`
		Services               struct {
			MaxUnits       int      `json:"max_units"`
			TimeoutSeconds int      `json:"timeout_seconds"`
			States         []string `json:"states"`
			EnabledStates  []string `json:"enabled_states"`
		} `json:"services"`
		Ports struct {
			MaxPorts        int      `json:"max_ports"`
			TimeoutSeconds  int      `json:"timeout_seconds"`
			ListeningStates []string `json:"listening_states"`
		} `json:"ports"`
	} `json:"system_inventory"`
	PreferencesSync struct {
		IntervalSecondsDefault int    `json:"interval_seconds_default"`
		StatePath              string `json:"state_path"`
	} `json:"preferences_sync"`
	Commands struct {
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
			AllowRestartAgent     bool `json:"allow_restart_agent"`
			AllowPackageUpdates   bool `json:"allow_package_updates"`
			AllowRebootDevice     bool `json:"allow_reboot_device"`
			AllowDockerRead       bool `json:"allow_docker_read"`
			AllowDockerLifecycle  bool `json:"allow_docker_lifecycle"`
			AllowDockerExec       bool `json:"allow_docker_exec"`
			AllowDockerPrune      bool `json:"allow_docker_prune"`
			AllowDockerCompose    bool `json:"allow_docker_compose"`
			CommandTimeoutSeconds int  `json:"command_timeout_seconds"`
		} `json:"policy_defaults"`
	} `json:"commands"`
	Resilience struct {
		HTTPTimeoutSeconds    int     `json:"http_timeout_seconds"`
		BackoffBaseMultiplier int     `json:"backoff_base_multiplier"`
		BackoffMaxSeconds     int     `json:"backoff_max_seconds"`
		JitterFraction        float64 `json:"jitter_fraction"`
	} `json:"resilience"`
	ServerLimitsMirrored struct {
		MaxRequestBytes    int64 `json:"max_request_bytes"`
		MaxFileBytesServer int64 `json:"max_file_bytes_server"`
		MaxSaveBytesServer int64 `json:"max_save_bytes_server"`
	} `json:"server_limits_mirrored"`
	SecurityAudit struct {
		Enabled             bool `json:"enabled"`
		LynisTimeoutSeconds int  `json:"lynis_timeout_seconds"`
		ReportMaxBytes      int  `json:"report_max_bytes"`
	} `json:"security_audit"`
}

// SaveUploadFileBytes is the smaller of the local and mirrored server caps
// for a single save-game file.
func (c *Contract) SaveUploadFileBytes() int64 {
	limit := c.Collection.Saves.MaxSaveFileBytes
	if c.ServerLimitsMirrored.MaxSaveBytesServer > 0 && c.ServerLimitsMirrored.MaxSaveBytesServer < limit {
		limit = c.ServerLimitsMirrored.MaxSaveBytesServer
	}
	return limit
}

// DockerLogsMaxTail caps how many container log lines a logs request may fetch.
func (c *Contract) DockerLogsMaxTail() int {
	if c.Commands.DockerLogsMaxTail > 0 {
		return c.Commands.DockerLogsMaxTail
	}
	return 2000
}

// DockerExecTimeout is the hard timeout for a docker exec start call.
func (c *Contract) DockerExecTimeout() time.Duration {
	seconds := c.Commands.DockerExecTimeoutSeconds
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

// Load parses the embedded contract. It never fails in practice; a broken
// embed is a build-time bug and surfaces as an error here.
func Load() (*Contract, error) {
	var c Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse embedded contract: %w", err)
	}
	return &c, nil
}

// MustLoad panics on an unreadable embedded contract.
func MustLoad() *Contract {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return c
}

// ExpandPath resolves a contract path like "~/.config/sync-win/policy.json"
// against the user's home directory and environment.
func ExpandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return os.ExpandEnv(path)
}

// StatePath returns the agent state file location, honoring XDG_STATE_HOME.
func StatePath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "sync-win", "agent-state.json")
	}
	return ExpandPath(cachedContract.PreferencesSync.StatePath)
}

// cachedContract backs StatePath; loaded lazily by SetStateSource/SetContract.
var cachedContract = mustLoadForPaths()

func mustLoadForPaths() Contract {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return *c
}

// Durations helpers keep call sites terse.

func (c *Contract) HTTPTimeout() time.Duration {
	return time.Duration(c.Resilience.HTTPTimeoutSeconds) * time.Second
}

func (c *Contract) BackoffMax() time.Duration {
	return time.Duration(c.Resilience.BackoffMaxSeconds) * time.Second
}

func (c *Contract) CommandTimeout(overrideSeconds int) time.Duration {
	if overrideSeconds > 0 {
		return time.Duration(overrideSeconds) * time.Second
	}
	return time.Duration(c.Commands.TimeoutSecondsDefault) * time.Second
}

func (c *Contract) LynisTimeout() time.Duration {
	if c.SecurityAudit.LynisTimeoutSeconds > 0 {
		return time.Duration(c.SecurityAudit.LynisTimeoutSeconds) * time.Second
	}
	return 600 * time.Second
}

// ServicesTimeout bounds one `systemctl` enumeration pass.
func (c *Contract) ServicesTimeout() time.Duration {
	if c.SystemInventory.Services.TimeoutSeconds > 0 {
		return time.Duration(c.SystemInventory.Services.TimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

// PortsTimeout bounds one `ss` snapshot.
func (c *Contract) PortsTimeout() time.Duration {
	if c.SystemInventory.Ports.TimeoutSeconds > 0 {
		return time.Duration(c.SystemInventory.Ports.TimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

// MaxServiceUnits caps how many systemd units a single collection may report.
func (c *Contract) MaxServiceUnits() int {
	if c.SystemInventory.Services.MaxUnits > 0 {
		return c.SystemInventory.Services.MaxUnits
	}
	return 400
}

// MaxOpenPorts caps how many listening sockets a single collection may report.
func (c *Contract) MaxOpenPorts() int {
	if c.SystemInventory.Ports.MaxPorts > 0 {
		return c.SystemInventory.Ports.MaxPorts
	}
	return 200
}
