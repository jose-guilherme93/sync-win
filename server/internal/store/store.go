package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

const (
	maxFileBytes      = 1 << 20
	maxFilenameLength = 255
	maxCategoryLength = 64
	maxRelPathLength  = 512
	maxFileTotalBytes = 10 << 20
	maxBatchItems     = 256
	maxExtraDirs      = 20
	maxExtraDirLength = 300
	maxDeviceTags     = 20
	maxPendingUpdates = 1000
	// Sessions last 30 days by default; override with SYNCWIN_SESSION_TTL_HOURS.
	defaultSessionTTL = 30 * 24 * time.Hour
	deviceColumns     = "id, user_id, owner_id, hostname, device_token, last_seen_at, last_sync_at, sync_failures, last_error, last_error_at, hardware_json, apps_json, status, created_at, updated_at, hardware_fingerprint, display_name, tags_json, collection_interval_seconds"
)

// sessionTTL is the session lifetime; it can be overridden at startup.
var sessionTTL = defaultSessionTTL

var (
	errInvalidFilename    = errors.New("invalid filename")
	errInvalidCategory    = errors.New("invalid category")
	errInvalidPath        = errors.New("invalid path")
	errPathTraversal      = errors.New("path traversal")
	errFilenameTooLong    = errors.New("filename too long")
	errCategoryTooLong    = errors.New("category too long")
	errRelPathTooLong     = errors.New("relative path too long")
	errFileTooLarge       = errors.New("file too large")
	errBinaryContent      = errors.New("binary content")
	errSecretContent      = errors.New("secret content")
	errSensitiveFilename  = errors.New("sensitive filename")
	errInvalidCategoryFmt = errors.New("invalid category format")
)

type Store struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

type Device struct {
	ID                        string        `json:"id"`
	UserID                    string        `json:"user_id"`
	OwnerID                   string        `json:"owner_id"`
	Hostname                  string        `json:"hostname"`
	DeviceToken               string        `json:"-"`
	LastSeenAt                time.Time     `json:"last_seen_at"`
	LastSyncAt                time.Time     `json:"last_sync_at"`
	SyncFailures              int           `json:"sync_failures"`
	LastError                 string        `json:"last_error"`
	LastErrorAt               time.Time     `json:"last_error_at"`
	Hardware                  HardwareStats `json:"hardware"`
	Apps                      []AppInfo     `json:"apps"`
	CreatedAt                 time.Time     `json:"created_at"`
	UpdatedAt                 time.Time     `json:"updated_at"`
	Status                    string        `json:"status"`
	HardwareFingerprint       string        `json:"-"`
	DisplayName               string        `json:"display_name,omitempty"`
	Tags                      []string      `json:"tags"`
	CollectionIntervalSeconds int           `json:"collection_interval_seconds"`
}

type DeviceSettings struct {
	DisplayName               string   `json:"display_name"`
	Tags                      []string `json:"tags"`
	CollectionIntervalSeconds int      `json:"collection_interval_seconds"`
}

type PendingUpdate struct {
	Source         string `json:"source"`
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version,omitempty"`
	NewVersion     string `json:"new_version"`
}

type UpdateInventory struct {
	Status    string          `json:"status"`
	CheckedAt string          `json:"checked_at,omitempty"`
	Message   string          `json:"message,omitempty"`
	Updates   []PendingUpdate `json:"updates"`
}

// DeviceSummary is the lightweight device projection served by the dashboard
// list endpoint. It omits heavy fields (app inventory, device token, system
// logs, top processes, Docker containers, per-core usage) that are only needed
// when a single device is opened.
type DeviceSummary struct {
	ID                        string          `json:"id"`
	UserID                    string          `json:"user_id"`
	OwnerID                   string          `json:"owner_id"`
	Hostname                  string          `json:"hostname"`
	LastSeenAt                time.Time       `json:"last_seen_at"`
	LastSyncAt                time.Time       `json:"last_sync_at"`
	SyncFailures              int             `json:"sync_failures"`
	LastError                 string          `json:"last_error"`
	LastErrorAt               time.Time       `json:"last_error_at"`
	Status                    string          `json:"status"`
	Hardware                  HardwareSummary `json:"hardware"`
	PreferenceCount           int             `json:"preference_count"`
	AppCount                  int             `json:"app_count"`
	SavesCount                int             `json:"saves_count"`
	SavesSizeBytes            int64           `json:"saves_size_bytes"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
	HardwareFingerprint       string          `json:"-"`
	DisplayName               string          `json:"display_name,omitempty"`
	Tags                      []string        `json:"tags"`
	CollectionIntervalSeconds int             `json:"collection_interval_seconds"`
}

// HardwareSummary is the subset of HardwareStats the dashboard list renders.
type HardwareSummary struct {
	CPUUsagePercent    float64         `json:"cpu_usage_percent"`
	MemoryUsedBytes    uint64          `json:"memory_used_bytes"`
	MemoryTotalBytes   uint64          `json:"memory_total_bytes"`
	CPUTemperature     float64         `json:"cpu_temperature"`
	PowerWatts         float64         `json:"power_watts"`
	BatteryPercent     float64         `json:"battery_percent"`
	BatteryStatus      string          `json:"battery_status"`
	AgentCPUUsage      float64         `json:"agent_cpu_usage"`
	AgentMemoryBytes   uint64          `json:"agent_memory_bytes"`
	AgentVersion       string          `json:"agent_version,omitempty"`
	KernelVersion      string          `json:"kernel_version"`
	OperatingSystem    string          `json:"operating_system"`
	DesktopEnvironment string          `json:"desktop_environment"`
	UptimeSeconds      int64           `json:"uptime_seconds"`
	LoadAverage        string          `json:"load_average"`
	NetworkIFaces      []NetworkIface  `json:"network_ifaces,omitempty"`
	NetRxRate          float64         `json:"net_rx_rate"`
	NetTxRate          float64         `json:"net_tx_rate"`
	DiskReadRate       float64         `json:"disk_read_rate"`
	DiskWriteRate      float64         `json:"disk_write_rate"`
	DiskPartitions     []DiskPartition `json:"disk_partitions,omitempty"`
	SwapUsedBytes      uint64          `json:"swap_used_bytes"`
	SwapTotalBytes     uint64          `json:"swap_total_bytes"`
	MemoryBuffersBytes uint64          `json:"memory_buffers_bytes"`
	MemoryCachedBytes  uint64          `json:"memory_cached_bytes"`
	// Per-core usage is a small list of floats and the dashboard renders it as
	// one bar per core, so it belongs on the card. Top processes and the Docker
	// container list stay out of the list payload on purpose: they are kilobytes
	// each and the device modal already loads them on demand.
	CPUCoreUsage    []float64   `json:"cpu_core_usage,omitempty"`
	GPUTemperature  float64     `json:"gpu_temperature_celsius"`
	DockerAvailable bool        `json:"docker_available"`
	DockerInfo      *DockerInfo `json:"docker_info,omitempty"`
	LynisAvailable  bool        `json:"lynis_available"`
	CollectedAt     string      `json:"collected_at"`
}

type HardwareStats struct {
	CPUUsagePercent    float64           `json:"cpu_usage_percent"`
	MemoryUsedBytes    uint64            `json:"memory_used_bytes"`
	MemoryTotalBytes   uint64            `json:"memory_total_bytes"`
	CPUTemperature     float64           `json:"cpu_temperature"`
	GPUTemperature     float64           `json:"gpu_temperature_celsius"`
	PowerWatts         float64           `json:"power_watts"`
	BatteryPercent     float64           `json:"battery_percent"`
	BatteryStatus      string            `json:"battery_status"`
	AgentCPUUsage      float64           `json:"agent_cpu_usage"`
	AgentMemoryBytes   uint64            `json:"agent_memory_bytes"`
	AgentVersion       string            `json:"agent_version,omitempty"`
	OperatingSystem    string            `json:"operating_system"`
	Architecture       string            `json:"architecture"`
	CPUModel           string            `json:"cpu_model"`
	KernelVersion      string            `json:"kernel_version"`
	DesktopEnvironment string            `json:"desktop_environment"`
	Locale             string            `json:"locale"`
	Timezone           string            `json:"timezone"`
	BootTime           string            `json:"boot_time"`
	UptimeSeconds      int64             `json:"uptime_seconds"`
	LoadAverage        string            `json:"load_average"`
	NetworkIFaces      []NetworkIface    `json:"network_ifaces,omitempty"`
	NetRxRate          float64           `json:"net_rx_rate"`
	NetTxRate          float64           `json:"net_tx_rate"`
	DiskReadBytes      uint64            `json:"disk_read_bytes"`
	DiskWriteBytes     uint64            `json:"disk_write_bytes"`
	DiskReadRate       float64           `json:"disk_read_rate"`
	DiskWriteRate      float64           `json:"disk_write_rate"`
	DiskPartitions     []DiskPartition   `json:"disk_partitions,omitempty"`
	SwapUsedBytes      uint64            `json:"swap_used_bytes"`
	SwapTotalBytes     uint64            `json:"swap_total_bytes"`
	MemoryBuffersBytes uint64            `json:"memory_buffers_bytes"`
	MemoryCachedBytes  uint64            `json:"memory_cached_bytes"`
	CPUCoreUsage       []float64         `json:"cpu_core_usage,omitempty"`
	TopCPUProcesses    []ProcessInfo     `json:"top_cpu_processes,omitempty"`
	TopMemProcesses    []ProcessInfo     `json:"top_mem_processes,omitempty"`
	DockerAvailable    bool              `json:"docker_available"`
	DockerContainers   []DockerContainer `json:"docker_containers,omitempty"`
	DockerInfo         *DockerInfo       `json:"docker_info,omitempty"`
	LynisAvailable     bool              `json:"lynis_available"`
	LynisInstallCmd    string            `json:"lynis_install_cmd,omitempty"`
	Logs               []DeviceLog       `json:"logs,omitempty"`
	// LogsStatus is the agent's explanation for having no logs, carried in the
	// telemetry payload so the dashboard can distinguish an unreadable journal
	// from a device with nothing to report.
	LogsStatus          string `json:"logs_status,omitempty"`
	HardwareFingerprint string `json:"-"`
	CollectedAt         string `json:"collected_at"`
}

type DiskPartition struct {
	Mount       string  `json:"mount"`
	Device      string  `json:"device"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type ProcessInfo struct {
	PID         int     `json:"pid"`
	Name        string  `json:"name"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemRSSBytes uint64  `json:"mem_rss_bytes"`
}

type DeviceLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

type DockerContainer struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Image  string        `json:"image"`
	State  string        `json:"state"`
	Status string        `json:"status"`
	Ports  []DockerPort  `json:"ports,omitempty"`
	Mounts []DockerMount `json:"mounts,omitempty"`
}

type DockerPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

type DockerMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
	Name        string `json:"name,omitempty"`
}

type DockerInfo struct {
	Version string `json:"version"`
	Total   int    `json:"total"`
	Running int    `json:"running"`
	Stopped int    `json:"stopped"`
	Paused  int    `json:"paused"`
	Images  int    `json:"images"`
	Driver  string `json:"driver"`
	NCPU    int    `json:"ncpu"`
}

type NetworkIface struct {
	Name      string  `json:"name"`
	RXBytes   uint64  `json:"rx_bytes"`
	TXBytes   uint64  `json:"tx_bytes"`
	RXRate    float64 `json:"rx_rate"`
	TXRate    float64 `json:"tx_rate"`
	RXPackets uint64  `json:"rx_packets"`
	TXPackets uint64  `json:"tx_packets"`
	RXErrors  uint64  `json:"rx_errors"`
	TXErrors  uint64  `json:"tx_errors"`
}

type AppInfo struct {
	Source  string `json:"source"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

// ServiceUnit mirrors the agent payload for one systemd service. Status is the
// bucket the dashboard colors by (running, failed, stopped); the raw systemd
// columns are kept so a detail view can show what the agent actually read.
type ServiceUnit struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	LoadState     string `json:"load_state"`
	ActiveState   string `json:"active_state"`
	SubState      string `json:"sub_state,omitempty"`
	UnitFileState string `json:"unit_file_state,omitempty"`
	Description   string `json:"description,omitempty"`
	Enabled       bool   `json:"enabled"`
}

// OpenPort mirrors one listening socket. Only the owning process name and pid
// are stored, never a command line.
type OpenPort struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local_address"`
	Port     int    `json:"port"`
	Process  string `json:"process,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

type PreferenceInput struct {
	Category     string `json:"category"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
	Encoding     string `json:"encoding,omitempty"`
}

type PreferenceFile struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"device_id"`
	UserID       string    `json:"user_id"`
	Category     string    `json:"category"`
	Filename     string    `json:"filename"`
	RelativePath string    `json:"relative_path"`
	Content      string    `json:"content"`
	Encoding     string    `json:"encoding,omitempty"`
	ContentHash  string    `json:"content_hash"`
	SizeBytes    int64     `json:"size_bytes"`
	SyncedAt     time.Time `json:"synced_at"`
	Status       string    `json:"status"`
}

type PreferenceRejection struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

type RestoreSaveFile struct {
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
	Encoding     string `json:"encoding,omitempty"`
}

type RestoreSavesPayload struct {
	SourceDeviceID string            `json:"source_device_id"`
	PrefixID       string            `json:"prefix_id"`
	GameName       string            `json:"game_name"`
	Files          []RestoreSaveFile `json:"files"`
}

type DeviceCommand struct {
	ID          string    `json:"id"`
	DeviceID    string    `json:"device_id"`
	Type        string    `json:"type"`
	Path        string    `json:"path,omitempty"`
	Name        string    `json:"name,omitempty"`
	Source      string    `json:"source,omitempty"`
	Payload     string    `json:"payload,omitempty"`
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	Token     string    `json:"token"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

func NewStore(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("store root is required")
	}
	if hours := strings.TrimSpace(os.Getenv("SYNCWIN_SESSION_TTL_HOURS")); hours != "" {
		if parsed, err := strconv.Atoi(hours); err == nil && parsed > 0 {
			sessionTTL = time.Duration(parsed) * time.Hour
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create store root: %w", err)
	}
	dbPath := filepath.Join(root, "sync-win.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db, path: root}
	if err := s.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	s.migrateAddFileEncodingColumn()
	s.migrateAddWorkspaceDirsColumn()
	s.migrateAddStatusColumn()
	s.migrateAddFingerprintColumn()
	if err := s.migrateAddSystemInventoryColumns(context.Background()); err != nil {
		return nil, err
	}
	if err := s.migrateAddDeviceSettingsColumns(context.Background()); err != nil {
		return nil, err
	}
	if err := s.migrateAddPendingUpdatesColumn(context.Background()); err != nil {
		return nil, err
	}
	s.migrateHashDeviceTokens()
	s.migrateAddSecurityAuditsTable()
	s.migrateAddLogsOwnerColumn()
	if err := migrateLegacyJSON(db, root); err != nil {
		return nil, fmt.Errorf("migrate legacy: %w", err)
	}
	return s, nil
}

func (s *Store) migrateAddWorkspaceDirsColumn() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('sync_config') WHERE name='workspace_dirs'").Scan(&count)
	if count == 0 {
		_, _ = s.db.Exec("ALTER TABLE sync_config ADD COLUMN workspace_dirs TEXT NOT NULL DEFAULT '[]'")
	}
}

func (s *Store) migrateAddFileEncodingColumn() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('files') WHERE name='encoding'").Scan(&count)
	if count == 0 {
		_, _ = s.db.Exec("ALTER TABLE files ADD COLUMN encoding TEXT NOT NULL DEFAULT ''")
	}
}

// migrateAddLogsOwnerColumn materialises the log owner on the row itself.
// Scoping a log query used to be `(user_id = ? OR device_id IN (SELECT id FROM
// devices WHERE owner_id = ?))`, and that OR with a subquery defeats every
// index on the table, so each page of the log viewer scanned the whole table.
// A plain owner_id column keeps the filter indexable.
func (s *Store) migrateAddLogsOwnerColumn() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('logs') WHERE name='owner_id'").Scan(&count)
	if count == 0 {
		if _, err := s.db.Exec("ALTER TABLE logs ADD COLUMN owner_id TEXT NOT NULL DEFAULT ''"); err != nil {
			return
		}
	}
	// Backfill from the device that produced each entry, falling back to the
	// user for entries that are not device-scoped.
	_, _ = s.db.Exec(`UPDATE logs SET owner_id = COALESCE(
		(SELECT d.owner_id FROM devices d WHERE d.id = logs.device_id), logs.user_id)
		WHERE owner_id = ''`)
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_logs_owner_ts ON logs(owner_id, ts)")
}

// migrateAddSystemInventoryColumns adds the services and ports snapshots. Both
// are plain JSON columns on the device row, mirroring apps_json: the agent
// replaces them wholesale on every upload, so there is nothing to join or
// garbage collect.
//
// Unlike the older migrations in this file, a failure here is returned rather
// than swallowed. Booting without the columns would leave the system inventory
// silently broken, which is worse than refusing to start.
//
// The context is explicit even though the caller only ever passes a background
// one: schema migrations run at boot and have no request to cancel, and stating
// that at the call site is better than letting database/sql imply it.
func (s *Store) migrateAddSystemInventoryColumns(ctx context.Context) error {
	for _, column := range []struct{ name, definition string }{
		{"services_json", "TEXT"},
		{"ports_json", "TEXT"},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name=?", column.name).Scan(&count); err != nil {
			return fmt.Errorf("inspect devices.%s: %w", column.name, err)
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE devices ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("add devices.%s: %w", column.name, err)
			}
		}
	}
	return nil
}

func (s *Store) migrateAddDeviceSettingsColumns(ctx context.Context) error {
	for _, column := range []struct{ name, definition string }{
		{"display_name", "TEXT NOT NULL DEFAULT ''"},
		{"tags_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"collection_interval_seconds", "INTEGER NOT NULL DEFAULT 10"},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name=?", column.name).Scan(&count); err != nil {
			return fmt.Errorf("inspect devices.%s: %w", column.name, err)
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE devices ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("add devices.%s: %w", column.name, err)
			}
		}
	}
	return nil
}

func (s *Store) migrateAddPendingUpdatesColumn(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='updates_json'").Scan(&count); err != nil {
		return fmt.Errorf("inspect devices.updates_json: %w", err)
	}
	if count == 0 {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE devices ADD COLUMN updates_json TEXT NOT NULL DEFAULT '{"status":"not_reported","updates":[]}'`); err != nil {
			return fmt.Errorf("add devices.updates_json: %w", err)
		}
	}
	return nil
}

func (s *Store) migrateAddStatusColumn() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='status'").Scan(&count)
	if count == 0 {
		s.db.Exec("ALTER TABLE devices ADD COLUMN status TEXT NOT NULL DEFAULT 'online'")
	}
}

func (s *Store) migrateAddFingerprintColumn() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='hardware_fingerprint'").Scan(&count)
	if count == 0 {
		s.db.Exec("ALTER TABLE devices ADD COLUMN hardware_fingerprint TEXT DEFAULT ''")
		s.db.Exec("CREATE INDEX IF NOT EXISTS idx_devices_fingerprint ON devices(hardware_fingerprint)")
	}
}

func (s *Store) migrateHashDeviceTokens() {
	rows, err := s.db.Query("SELECT id, device_token FROM devices")
	if err != nil {
		return
	}
	type pending struct{ id, token string }
	var updates []pending
	for rows.Next() {
		var id, token string
		if rows.Scan(&id, &token) == nil && token != "" && !isTokenHash(token) {
			updates = append(updates, pending{id: id, token: hashToken(token)})
		}
	}
	rows.Close()
	for _, update := range updates {
		_, _ = s.db.Exec("UPDATE devices SET device_token = ? WHERE id = ?", update.token, update.id)
	}
}

func (s *Store) migrateAddSecurityAuditsTable() {
	var exists int
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='security_audits'").Scan(&exists)
	if exists == 0 {
		s.db.Exec(`CREATE TABLE IF NOT EXISTS security_audits (
			id                TEXT PRIMARY KEY,
			device_id         TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			owner_id          TEXT NOT NULL,
			hardening_index   INTEGER NOT NULL DEFAULT 0,
			total_warnings    INTEGER NOT NULL DEFAULT 0,
			total_suggestions INTEGER NOT NULL DEFAULT 0,
			total_tests       INTEGER NOT NULL DEFAULT 0,
			tests_passed      INTEGER NOT NULL DEFAULT 0,
			lynis_version     TEXT NOT NULL DEFAULT '',
			os_info           TEXT NOT NULL DEFAULT '',
			kernel_version    TEXT NOT NULL DEFAULT '',
			report_json       TEXT NOT NULL DEFAULT '{}',
			created_at        TEXT NOT NULL DEFAULT (datetime('now'))
		)`)
		s.db.Exec("CREATE INDEX IF NOT EXISTS idx_security_audits_device ON security_audits(device_id, created_at DESC)")
		s.db.Exec("CREATE INDEX IF NOT EXISTS idx_security_audits_owner ON security_audits(owner_id, created_at DESC)")
	}
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		hostname TEXT NOT NULL,
		device_token TEXT NOT NULL UNIQUE,
		last_seen_at TEXT,
		last_sync_at TEXT,
		sync_failures INTEGER NOT NULL DEFAULT 0,
		last_error TEXT,
		last_error_at TEXT,
		hardware_json TEXT,
		apps_json TEXT,
		services_json TEXT,
		ports_json TEXT,
		status TEXT NOT NULL DEFAULT 'online',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		tags_json TEXT NOT NULL DEFAULT '[]',
		collection_interval_seconds INTEGER NOT NULL DEFAULT 10,
		updates_json TEXT NOT NULL DEFAULT '{"status":"not_reported","updates":[]}'
	);
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		category TEXT NOT NULL,
		filename TEXT NOT NULL,
		relative_path TEXT NOT NULL,
		content TEXT NOT NULL,
		encoding TEXT NOT NULL DEFAULT '',
		content_hash TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		synced_at TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'synced'
	);
	CREATE TABLE IF NOT EXISTS commands (
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		type TEXT NOT NULL,
		path TEXT,
		name TEXT,
		source TEXT,
		payload TEXT,
		status TEXT NOT NULL DEFAULT 'queued',
		message TEXT,
		created_at TEXT NOT NULL,
		completed_at TEXT
	);
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS telemetry_raw (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		payload TEXT NOT NULL,
		received_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS telemetry_downsampled (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		resolution TEXT NOT NULL,
		cpu_avg REAL,
		cpu_min REAL,
		cpu_max REAL,
		mem_avg REAL,
		mem_min REAL,
		mem_max REAL,
		net_rx_avg REAL,
		net_tx_avg REAL,
		temp_avg REAL,
		temp_max REAL,
		power_avg REAL,
		sample_count INTEGER
	);
	CREATE TABLE IF NOT EXISTS notification_configs (
		owner_id TEXT NOT NULL,
		provider TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		config_enc TEXT,
		events TEXT,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (owner_id, provider)
	);
	CREATE TABLE IF NOT EXISTS notification_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL,
		type TEXT NOT NULL,
		device_id TEXT,
		hostname TEXT,
		message TEXT NOT NULL,
		read INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_files_device ON files(device_id);
	CREATE INDEX IF NOT EXISTS idx_files_hash ON files(device_id, content_hash);
	CREATE INDEX IF NOT EXISTS idx_commands_device ON commands(device_id);
	CREATE INDEX IF NOT EXISTS idx_commands_status ON commands(device_id, status);
	CREATE INDEX IF NOT EXISTS idx_telemetry_raw_device ON telemetry_raw(device_id);
	CREATE INDEX IF NOT EXISTS idx_telemetry_downsampled_device ON telemetry_downsampled(device_id, resolution);
	CREATE INDEX IF NOT EXISTS idx_notification_events_owner ON notification_events(owner_id);
	CREATE TABLE IF NOT EXISTS enrollment_tokens (
		id         TEXT PRIMARY KEY,
		owner_id   TEXT NOT NULL,
		token      TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		used_at    TEXT NOT NULL DEFAULT '',
		device_id  TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS sync_config (
		owner_id TEXT PRIMARY KEY,
		extra_dirs TEXT NOT NULL DEFAULT '[]',
		workspace_dirs TEXT NOT NULL DEFAULT '[]',
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS retention_settings (
		owner_id TEXT PRIMARY KEY,
		raw_hours INTEGER NOT NULL DEFAULT 2,
		resolution_1m_days INTEGER NOT NULL DEFAULT 7,
		resolution_5m_days INTEGER NOT NULL DEFAULT 30,
		resolution_1h_days INTEGER NOT NULL DEFAULT 365,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS device_notes (
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS device_attachments (
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		caption TEXT,
		data BLOB NOT NULL,
		size_bytes INTEGER NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS security_audits (
		id                TEXT PRIMARY KEY,
		device_id         TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		owner_id          TEXT NOT NULL,
		hardening_index   INTEGER NOT NULL DEFAULT 0,
		total_warnings    INTEGER NOT NULL DEFAULT 0,
		total_suggestions INTEGER NOT NULL DEFAULT 0,
		total_tests       INTEGER NOT NULL DEFAULT 0,
		tests_passed      INTEGER NOT NULL DEFAULT 0,
		lynis_version     TEXT NOT NULL DEFAULT '',
		os_info           TEXT NOT NULL DEFAULT '',
		kernel_version    TEXT NOT NULL DEFAULT '',
		report_json       TEXT NOT NULL DEFAULT '{}',
		created_at        TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_security_audits_device ON security_audits(device_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_security_audits_owner ON security_audits(owner_id, created_at DESC);
	CREATE TABLE IF NOT EXISTS device_logs (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id  TEXT NOT NULL,
		owner_id   TEXT NOT NULL DEFAULT '',
		ts         TEXT NOT NULL,
		level      TEXT NOT NULL DEFAULT 'info',
		source     TEXT NOT NULL DEFAULT '',
		message    TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_device_logs_device_ts ON device_logs(device_id, ts DESC);
	CREATE INDEX IF NOT EXISTS idx_device_logs_owner_ts ON device_logs(owner_id, ts DESC);
	CREATE INDEX IF NOT EXISTS idx_device_logs_device_level_ts ON device_logs(device_id, level, ts DESC);
	CREATE INDEX IF NOT EXISTS idx_device_logs_device_source_ts ON device_logs(device_id, source, ts DESC);
	-- The agent re-ships overlapping journal windows every sample, so the same
	-- line arrives many times. This unique key is what makes ingestion
	-- idempotent; without it the table grows without bound.
	CREATE UNIQUE INDEX IF NOT EXISTS idx_device_logs_dedupe ON device_logs(device_id, ts, source, message);
	CREATE TABLE IF NOT EXISTS logs (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		ts             TEXT NOT NULL,
		level          TEXT NOT NULL,
		category       TEXT NOT NULL,
		event          TEXT NOT NULL,
		message        TEXT NOT NULL DEFAULT '',
		device_id      TEXT NOT NULL DEFAULT '',
		user_id        TEXT NOT NULL DEFAULT '',
		request_id     TEXT NOT NULL DEFAULT '',
		correlation_id TEXT NOT NULL DEFAULT '',
		duration_ms    INTEGER NOT NULL DEFAULT 0,
		status         INTEGER NOT NULL DEFAULT 0,
		metadata       TEXT NOT NULL DEFAULT '{}',
		redacted       INTEGER NOT NULL DEFAULT 0,
		owner_id       TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts);
	CREATE INDEX IF NOT EXISTS idx_logs_level ON logs(level);
	CREATE INDEX IF NOT EXISTS idx_logs_device ON logs(device_id, ts);
	CREATE INDEX IF NOT EXISTS idx_logs_request ON logs(request_id);
	CREATE INDEX IF NOT EXISTS idx_logs_correlation ON logs(correlation_id);
	CREATE INDEX IF NOT EXISTS idx_logs_event ON logs(event);
	CREATE INDEX IF NOT EXISTS idx_logs_level_ts ON logs(level, ts);
	CREATE INDEX IF NOT EXISTS idx_logs_device_level ON logs(device_id, level, ts);
	-- idx_logs_owner_ts is deliberately NOT created here. On an existing
	-- database the CREATE TABLE above is a no-op, so owner_id does not exist
	-- yet and indexing it fails before the migration can add the column. The
	-- index is created in migrateAddLogsOwnerColumn instead.
	CREATE TABLE IF NOT EXISTS http_access (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		method          TEXT NOT NULL,
		path            TEXT NOT NULL,
		status_class    TEXT NOT NULL,
		count           INTEGER NOT NULL DEFAULT 1,
		avg_duration_ms REAL NOT NULL DEFAULT 0,
		max_duration_ms INTEGER NOT NULL DEFAULT 0,
		window_start    TEXT NOT NULL,
		window_end      TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_http_access_window ON http_access(window_start, window_end);
	CREATE INDEX IF NOT EXISTS idx_http_access_path ON http_access(path, window_start);
	CREATE TABLE IF NOT EXISTS metrics (
		id     INTEGER PRIMARY KEY AUTOINCREMENT,
		name   TEXT NOT NULL,
		value  REAL NOT NULL,
		ts     TEXT NOT NULL,
		labels TEXT NOT NULL DEFAULT '{}'
	);
	CREATE INDEX IF NOT EXISTS idx_metrics_name_ts ON metrics(name, ts);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Enforce one files row per (device, category, relative path, filename).
	// Older databases may hold duplicates produced before the dedup check was
	// fixed, so collapse them onto the most recent row before creating the index.
	var filesUniqueIndex int
	_ = s.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_files_unique'",
	).Scan(&filesUniqueIndex)
	if filesUniqueIndex == 0 {
		_, _ = s.db.Exec(`DELETE FROM files WHERE rowid NOT IN (
			SELECT MAX(rowid) FROM files GROUP BY device_id, category, relative_path, filename
		)`)
	}
	_, err := s.db.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_files_unique ON files(device_id, category, relative_path, filename)",
	)
	return err
}

func timeText(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func textTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (s *Store) RegisterDevice(hostname, userID, ownerID, fingerprint string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Hardware fingerprints are metadata only. They must never authenticate or
	// silently reconnect a device; enrollment always creates a fresh credential.
	if userID == "" {
		userID = "user-" + newID()
	}
	if ownerID == "" {
		ownerID = userID
	}
	if hostname == "" {
		hostname = "unknown"
	}
	if err := validateHostname(hostname); err != nil {
		return Device{}, err
	}

	// Do not reuse a device record based on owner/hostname either: a new
	// enrollment must always receive a new credential.
	token := newToken()

	now := time.Now().UTC()
	device := Device{
		ID:          newID(),
		UserID:      userID,
		OwnerID:     ownerID,
		Hostname:    hostname,
		DeviceToken: token,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		"INSERT INTO devices ("+deviceColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		device.ID, device.UserID, device.OwnerID, device.Hostname, hashToken(device.DeviceToken),
		"", "", 0, "", "", "{}", "[]", "online", timeText(now), timeText(now), fingerprint, "", "[]", 10,
	)
	if err != nil {
		return Device{}, err
	}
	return device, nil
}

func (s *Store) RecordHeartbeat(deviceID string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := timeText(time.Now().UTC())
	_, err := s.db.Exec(
		"UPDATE devices SET last_seen_at = ?, sync_failures = 0, status = 'online', updated_at = ? WHERE id = ?",
		now, now, deviceID,
	)
	if err != nil {
		return Device{}, err
	}
	return s.getDeviceLocked(deviceID)
}

func (s *Store) GetDevice(deviceID string) (Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getDeviceLocked(deviceID)
}

// UpdateDeviceStatus sets the status field for a device.
func (s *Store) UpdateDeviceStatus(deviceID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec(
		"UPDATE devices SET status = ?, updated_at = ? WHERE id = ?",
		status, now, deviceID,
	)
	return err
}

func (s *Store) FindDeviceByFingerprint(fingerprint string) (Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findDeviceByFingerprintLocked(fingerprint)
}

func (s *Store) findDeviceByOwnerHostnameLocked(ownerID, hostname string) (Device, error) {
	if ownerID == "" || hostname == "" {
		return Device{}, fmt.Errorf("empty owner_id or hostname")
	}
	row := s.db.QueryRow(
		"SELECT "+deviceColumns+" FROM devices WHERE owner_id = ? AND hostname = ? LIMIT 1",
		ownerID, hostname,
	)
	return scanDevice(row)
}

func (s *Store) findDeviceByFingerprintLocked(fingerprint string) (Device, error) {
	if fingerprint == "" {
		return Device{}, fmt.Errorf("empty fingerprint")
	}
	row := s.db.QueryRow(
		"SELECT "+deviceColumns+" FROM devices WHERE hardware_fingerprint = ? LIMIT 1",
		fingerprint,
	)
	return scanDevice(row)
}

func (s *Store) UpdateFingerprint(deviceID, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"UPDATE devices SET hardware_fingerprint = ?, updated_at = ? WHERE id = ?",
		fingerprint, timeText(time.Now().UTC()), deviceID,
	)
	return err
}

func (s *Store) UpdateDeviceHostname(deviceID, hostname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"UPDATE devices SET hostname = ?, updated_at = ? WHERE id = ?",
		hostname, timeText(time.Now().UTC()), deviceID,
	)
	return err
}

func (s *Store) FindDuplicateDevices(fingerprint string) ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if fingerprint == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		"SELECT "+deviceColumns+" FROM devices WHERE hardware_fingerprint = ? AND hardware_fingerprint != '' ORDER BY last_seen_at DESC",
		fingerprint,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []Device
	for rows.Next() {
		d, err := scanDeviceRow(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *Store) ListDevices() ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT " + deviceColumns + " FROM devices")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		d, err := scanDeviceRow(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func scanDeviceRow(row interface{ Scan(dest ...any) error }) (Device, error) {
	return scanDevice(row)
}

func scanDevice(row interface{ Scan(dest ...any) error }) (Device, error) {
	var d Device
	var lastSeen, lastSync, lastErrorAt, createdAt, updatedAt string
	var hardware, apps, tags string
	err := row.Scan(
		&d.ID, &d.UserID, &d.OwnerID, &d.Hostname, &d.DeviceToken,
		&lastSeen, &lastSync, &d.SyncFailures, &d.LastError, &lastErrorAt,
		&hardware, &apps, &d.Status, &createdAt, &updatedAt, &d.HardwareFingerprint,
		&d.DisplayName, &tags, &d.CollectionIntervalSeconds,
	)
	if err != nil {
		return Device{}, err
	}
	d.LastSeenAt = textTime(lastSeen)
	if lastSync != "" {
		d.LastSyncAt = textTime(lastSync)
	}
	if lastErrorAt != "" {
		d.LastErrorAt = textTime(lastErrorAt)
	}
	d.CreatedAt = textTime(createdAt)
	d.UpdatedAt = textTime(updatedAt)
	if hardware != "" {
		json.Unmarshal([]byte(hardware), &d.Hardware)
	}
	if apps != "" {
		json.Unmarshal([]byte(apps), &d.Apps)
	}
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &d.Tags)
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return d, nil
}

func (s *Store) getDeviceLocked(deviceID string) (Device, error) {
	row := s.db.QueryRow(
		"SELECT "+deviceColumns+" FROM devices WHERE id = ?", deviceID,
	)
	return scanDevice(row)
}

// GetDeviceDetail loads a single device with its full hardware payload but
// without the (potentially large) app inventory, which the dashboard fetches
// separately. The device token is not serialized (json:"-").
func (s *Store) GetDeviceDetail(deviceID string) (Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow(
		`SELECT id, user_id, owner_id, hostname, device_token, last_seen_at, last_sync_at,
		        sync_failures, last_error, last_error_at, hardware_json, '', status,
		        created_at, updated_at, hardware_fingerprint, display_name, tags_json,
		        collection_interval_seconds
		 FROM devices WHERE id = ?`, deviceID,
	)
	return scanDevice(row)
}

func (s *Store) ListFiles(deviceID string) ([]PreferenceFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT id, device_id, user_id, category, filename, relative_path, content, encoding, content_hash, size_bytes, synced_at, status FROM files WHERE device_id = ? ORDER BY synced_at DESC",
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]PreferenceFile, 0)
	for rows.Next() {
		var f PreferenceFile
		var syncedAt string
		if err := rows.Scan(&f.ID, &f.DeviceID, &f.UserID, &f.Category, &f.Filename, &f.RelativePath, &f.Content, &f.Encoding, &f.ContentHash, &f.SizeBytes, &syncedAt, &f.Status); err != nil {
			return nil, err
		}
		f.SyncedAt = textTime(syncedAt)
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) SavePreferenceBatch(deviceID string, inputs []PreferenceInput) ([]PreferenceFile, []PreferenceRejection, error) {
	if len(inputs) > maxBatchItems {
		return nil, nil, fmt.Errorf("too many preference files: maximum %d", maxBatchItems)
	}
	var totalBytes int64
	for _, input := range inputs {
		totalBytes += int64(len(input.Content))
		if totalBytes > maxFileTotalBytes {
			return nil, nil, fmt.Errorf("preference batch is too large: maximum %d bytes", maxFileTotalBytes)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.getDeviceLocked(deviceID); err != nil {
		return nil, nil, err
	}

	// One transaction per batch: a failure partway through must not leave the
	// device with a half-applied set of files.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var saved []PreferenceFile
	var rejected []PreferenceRejection

	for _, input := range inputs {
		if err := validatePreferenceInput(input); err != nil {
			rejected = append(rejected, PreferenceRejection{Filename: input.Filename, Reason: err.Error()})
			continue
		}
		decoded, err := decodePreferenceContent(input)
		if err != nil {
			rejected = append(rejected, PreferenceRejection{Filename: input.Filename, Reason: err.Error()})
			continue
		}
		hash := hashBytes(decoded)
		size := int64(len(decoded))
		var existing PreferenceFile
		var existingSyncedAt string
		err = tx.QueryRow(
			"SELECT id, device_id, user_id, category, filename, relative_path, content, encoding, content_hash, size_bytes, synced_at, status FROM files WHERE device_id = ? AND category = ? AND relative_path = ? AND filename = ?",
			deviceID, input.Category, input.RelativePath, input.Filename,
		).Scan(&existing.ID, &existing.DeviceID, &existing.UserID, &existing.Category, &existing.Filename, &existing.RelativePath, &existing.Content, &existing.Encoding, &existing.ContentHash, &existing.SizeBytes, &existingSyncedAt, &existing.Status)
		if err == nil && existing.ContentHash == hash && existing.Encoding == input.Encoding {
			continue
		}

		id := newID()
		if err == nil && existing.ID != "" {
			id = existing.ID
		}
		now := time.Now().UTC()
		_, err = tx.Exec(
			`INSERT INTO files (id, device_id, user_id, category, filename, relative_path, content, encoding, content_hash, size_bytes, synced_at, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(device_id, category, relative_path, filename) DO UPDATE SET
			   content = excluded.content,
			   encoding = excluded.encoding,
			   content_hash = excluded.content_hash,
			   size_bytes = excluded.size_bytes,
			   synced_at = excluded.synced_at,
			   status = excluded.status`,
			id, deviceID, "", input.Category, input.Filename, input.RelativePath, input.Content, input.Encoding, hash, size, timeText(now), "synced",
		)
		if err != nil {
			return nil, nil, fmt.Errorf("store preference %s: %w", input.Filename, err)
		}
		saved = append(saved, PreferenceFile{
			ID:           id,
			DeviceID:     deviceID,
			UserID:       "",
			Category:     input.Category,
			Filename:     input.Filename,
			RelativePath: input.RelativePath,
			Content:      input.Content,
			Encoding:     input.Encoding,
			ContentHash:  hash,
			SizeBytes:    size,
			SyncedAt:     now,
			Status:       "synced",
		})
	}

	// A completed sync batch marks the device as online and refreshes its
	// last sync timestamp, even when every file was unchanged.
	syncAt := timeText(time.Now().UTC())
	if _, err := tx.Exec(
		"UPDATE devices SET last_sync_at = ?, status = 'online', updated_at = ? WHERE id = ?",
		syncAt, syncAt, deviceID,
	); err != nil {
		return saved, rejected, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return saved, rejected, nil
}

func (s *Store) DeletePreferenceFile(deviceID, fileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM files WHERE id = ? AND device_id = ?", fileID, deviceID)
	return err
}

func (s *Store) UpdateHardwareStats(deviceID string, stats HardwareStats) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hwJSON, err := json.Marshal(stats)
	if err != nil {
		return Device{}, err
	}
	now := timeText(time.Now().UTC())
	// Save hardware_fingerprint to the dedicated column if provided and not yet set.
	if stats.HardwareFingerprint != "" {
		_, _ = s.db.Exec(
			"UPDATE devices SET hardware_fingerprint = ? WHERE id = ? AND (hardware_fingerprint IS NULL OR hardware_fingerprint = '')",
			stats.HardwareFingerprint, deviceID,
		)
		// Check for duplicate devices with the same fingerprint.
		s.checkDuplicateFingerprints(stats.HardwareFingerprint, deviceID)
	}
	_, err = s.db.Exec(
		"UPDATE devices SET hardware_json = ?, last_seen_at = ?, sync_failures = 0, last_error = '', status = 'online', updated_at = ? WHERE id = ?",
		string(hwJSON), now, now, deviceID,
	)
	if err != nil {
		return Device{}, err
	}
	return s.getDeviceLocked(deviceID)
}

// checkDuplicateFingerprints detects devices sharing the same hardware fingerprint.
// If duplicates are found, the oldest device (by last_seen_at) is marked as 'duplicate'
// so the user can decide which to keep.
func (s *Store) checkDuplicateFingerprints(fingerprint, currentDeviceID string) {
	rows, err := s.db.Query(
		"SELECT id, last_seen_at FROM devices WHERE hardware_fingerprint = ? AND hardware_fingerprint != '' AND id != ? ORDER BY last_seen_at DESC",
		fingerprint, currentDeviceID,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	now := timeText(time.Now().UTC())
	for rows.Next() {
		var dupID string
		var lastSeen time.Time
		if err := rows.Scan(&dupID, &lastSeen); err != nil {
			continue
		}
		// Mark the older device as duplicate.
		_, _ = s.db.Exec(
			"UPDATE devices SET status = 'duplicate', last_error = ?, last_error_at = ?, updated_at = ? WHERE id = ?",
			"Duplicate fingerprint detected — this device shares hardware with "+currentDeviceID,
			now, now, dupID,
		)
	}
}

func (s *Store) QueueCommand(deviceID, cmdType, path, name, source, payload string) (DeviceCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCommandLocked(deviceID, cmdType, path, name, source, payload)
}

func (s *Store) queueCommandLocked(deviceID, cmdType, path, name, source, payload string) (DeviceCommand, error) {
	command := DeviceCommand{
		ID:        newID(),
		DeviceID:  deviceID,
		Type:      cmdType,
		Path:      path,
		Name:      name,
		Source:    source,
		Payload:   payload,
		Status:    "queued",
		CreatedAt: time.Now().UTC(),
	}
	return command, s.insertCommand(command)
}

func (s *Store) QueueDockerCommand(deviceID, cmdType, name, source, payload string) (DeviceCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	validTypes := map[string]bool{
		"docker_list_containers": true, "docker_container_stats": true,
		"docker_container_logs": true, "docker_container_start": true,
		"docker_container_stop": true, "docker_container_restart": true,
		"docker_container_kill": true, "docker_container_remove": true,
		"docker_system_prune": true, "docker_image_prune": true,
		"docker_container_prune": true, "docker_network_prune": true,
		"docker_exec": true, "docker_compose_read": true, "docker_compose_write": true,
		"docker_compose_up": true, "docker_compose_down": true, "docker_compose_ps": true,
		"docker_compose_logs": true,
	}
	if !validTypes[cmdType] {
		return DeviceCommand{}, fmt.Errorf("unsupported docker command type: %s", cmdType)
	}
	command := DeviceCommand{
		ID:        newID(),
		DeviceID:  deviceID,
		Type:      cmdType,
		Name:      name,
		Source:    source,
		Payload:   payload,
		Status:    "queued",
		CreatedAt: time.Now().UTC(),
	}
	return command, s.insertCommand(command)
}

func (s *Store) insertCommand(command DeviceCommand) error {
	_, err := s.db.Exec(
		"INSERT INTO commands (id, device_id, type, path, name, source, payload, status, message, created_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		command.ID, command.DeviceID, command.Type, command.Path, command.Name, command.Source, command.Payload, command.Status, command.Message, timeText(command.CreatedAt), "",
	)
	return err
}

func (s *Store) GetPendingCommands(deviceID string) ([]DeviceCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getPendingCommandsLocked(deviceID)
}

func (s *Store) getPendingCommandsLocked(deviceID string) ([]DeviceCommand, error) {
	rows, err := s.db.Query(
		"SELECT id, device_id, type, path, name, source, payload, status, message, created_at, completed_at FROM commands WHERE device_id = ? AND status = 'queued' ORDER BY created_at ASC",
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commands []DeviceCommand
	for rows.Next() {
		var c DeviceCommand
		var createdAt, completedAt string
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Type, &c.Path, &c.Name, &c.Source, &c.Payload, &c.Status, &c.Message, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		c.CreatedAt = textTime(createdAt)
		if completedAt != "" {
			c.CompletedAt = textTime(completedAt)
		}
		commands = append(commands, c)
	}
	return commands, rows.Err()
}

func (s *Store) GetPendingDockerCommands(deviceID string) ([]DeviceCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT id, device_id, type, path, name, source, payload, status, message, created_at, completed_at FROM commands WHERE device_id = ? AND status = 'queued' AND type LIKE 'docker_%' ORDER BY created_at ASC",
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commands []DeviceCommand
	for rows.Next() {
		var c DeviceCommand
		var createdAt, completedAt string
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Type, &c.Path, &c.Name, &c.Source, &c.Payload, &c.Status, &c.Message, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		c.CreatedAt = textTime(createdAt)
		if completedAt != "" {
			c.CompletedAt = textTime(completedAt)
		}
		commands = append(commands, c)
	}
	return commands, rows.Err()
}

func (s *Store) CompleteCommand(commandID, deviceID, status, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	result, err := s.db.Exec(
		"UPDATE commands SET status = ?, message = ?, completed_at = ? WHERE id = ? AND device_id = ?",
		status, message, now, commandID, deviceID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("command not found or not owned by device")
	}
	return nil
}

// GetCommand returns a single command by ID and device.
func (s *Store) GetCommand(commandID, deviceID string) (DeviceCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c DeviceCommand
	var createdAt, completedAt string
	err := s.db.QueryRow(
		"SELECT id, device_id, type, path, name, source, payload, status, message, created_at, completed_at FROM commands WHERE id = ? AND device_id = ?",
		commandID, deviceID,
	).Scan(&c.ID, &c.DeviceID, &c.Type, &c.Path, &c.Name, &c.Source, &c.Payload, &c.Status, &c.Message, &createdAt, &completedAt)
	if err != nil {
		return c, err
	}
	c.CreatedAt = textTime(createdAt)
	if completedAt != "" {
		c.CompletedAt = textTime(completedAt)
	}
	return c, nil
}

// GetCommandType returns the type of a command.
func (s *Store) GetCommandType(commandID, deviceID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cmdType string
	err := s.db.QueryRow("SELECT type FROM commands WHERE id = ? AND device_id = ?", commandID, deviceID).Scan(&cmdType)
	return cmdType, err
}

func (s *Store) AppendTelemetryDownsampled(agg TelemetryDownsampled) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO telemetry_downsampled (device_id, timestamp, resolution, cpu_avg, cpu_min, cpu_max, mem_avg, mem_min, mem_max, net_rx_avg, net_tx_avg, temp_avg, temp_max, power_avg, sample_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agg.DeviceID, agg.Timestamp, agg.Resolution, agg.CPUAvg, agg.CPUMin, agg.CPUMax, agg.MemAvg, agg.MemMin, agg.MemMax, agg.NetRXAvg, agg.NetTXAvg, agg.TempAvg, agg.TempMax, agg.PowerAvg, agg.SampleCount,
	)
	return err
}

// maxSystemInventoryRows bounds one stored system inventory snapshot. It is the
// server-side ceiling and deliberately matches the contract's max_units so an
// agent cannot push a larger list than the contract documents.
const maxSystemInventoryRows = 400

// maxTelemetryRows bounds how many rows a single history range query can load
// into memory, regardless of the requested window.
const maxTelemetryRows = 5000

func (s *Store) GetDownsampledRange(deviceID, resolution string, from, to time.Time) ([]TelemetryDownsampled, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT id, device_id, timestamp, resolution, cpu_avg, cpu_min, cpu_max, mem_avg, mem_min, mem_max, net_rx_avg, net_tx_avg, temp_avg, temp_max, power_avg, sample_count FROM telemetry_downsampled WHERE device_id = ? AND resolution = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC LIMIT ?",
		deviceID, resolution, timeText(from), timeText(to), maxTelemetryRows,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TelemetryDownsampled
	for rows.Next() {
		var p TelemetryDownsampled
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Timestamp, &p.Resolution, &p.CPUAvg, &p.CPUMin, &p.CPUMax, &p.MemAvg, &p.MemMin, &p.MemMax, &p.NetRXAvg, &p.NetTXAvg, &p.TempAvg, &p.TempMax, &p.PowerAvg, &p.SampleCount); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func validatePreferenceInput(input PreferenceInput) error {
	if input.Filename == "" {
		return errInvalidFilename
	}
	if input.Content == "" {
		return errors.New("empty content")
	}
	if input.Filename == "." || input.Filename == ".." || strings.ContainsAny(input.Filename, "/\\\x00\r\n") {
		return errInvalidFilename
	}
	if len(input.Filename) > maxFilenameLength {
		return errFilenameTooLong
	}
	if isSensitiveFilename(input.Filename) {
		return errSensitiveFilename
	}
	if input.Category == "" {
		return errInvalidCategory
	}
	if len(input.Category) > maxCategoryLength {
		return errCategoryTooLong
	}
	if !validCategoryFormat(input.Category) {
		return errInvalidCategoryFmt
	}
	if input.RelativePath == "" {
		return errInvalidPath
	}
	if len(input.RelativePath) > maxRelPathLength {
		return errRelPathTooLong
	}
	if strings.Contains(input.RelativePath, "..") || strings.HasPrefix(input.RelativePath, "/") {
		return errPathTraversal
	}
	if input.Encoding != "" && input.Encoding != "base64" {
		return fmt.Errorf("unsupported content encoding: %s", input.Encoding)
	}
	if input.Encoding == "base64" {
		if input.Category != "saves" {
			return errors.New("base64 encoding is only valid for saves")
		}
		if len(input.Content) > base64.StdEncoding.EncodedLen(maxFileBytes) {
			return errFileTooLarge
		}
		decoded, err := base64.StdEncoding.DecodeString(input.Content)
		if err != nil {
			return errBinaryContent
		}
		if len(decoded) == 0 {
			return errors.New("empty content")
		}
		if len(decoded) > maxFileBytes {
			return errFileTooLarge
		}
		return nil
	}
	if int64(len(input.Content)) > maxFileBytes {
		return errFileTooLarge
	}
	if !utf8.ValidString(input.Content) {
		return errBinaryContent
	}
	if strings.ContainsRune(input.Content, '\x00') {
		return errBinaryContent
	}
	if containsSecret(input.Content) {
		return errSecretContent
	}
	return nil
}

var sensitiveNames = map[string]bool{
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	".netrc": true, ".pgpass": true, ".my.cnf": true,
}

func isSensitiveFilename(name string) bool {
	lower := strings.ToLower(name)
	if sensitiveNames[lower] {
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	return false
}

func validCategoryFormat(cat string) bool {
	for _, r := range cat {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func decodePreferenceContent(input PreferenceInput) ([]byte, error) {
	if input.Encoding == "base64" {
		return base64.StdEncoding.DecodeString(input.Content)
	}
	return []byte(input.Content), nil
}

func hashBytes(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func hashContent(content string) string {
	return hashBytes([]byte(content))
}

func containsSecret(content string) bool {
	for _, pattern := range secretPatterns {
		if strings.Contains(strings.ToLower(content), pattern) {
			return true
		}
	}
	return false
}

var secretPatterns = []string{
	"api_key", "apikey", "api_token", "access_token", "secret_key", "secret_token",
	"password", "passwd", "private_key", "ssh_key", "auth_token", "bearer_token",
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "glpat-", "aws_access_key", "aws_secret",
	"akia", "xoxb-", "xoxp-", "sk_live_", "rk_live_", "-----BEGIN PRIVATE KEY-----",
	"-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN OPENSSH PRIVATE KEY-----",
}

func mustRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("secure random source failed: %v", err))
	}
	return b
}

func newID() string {
	return hex.EncodeToString(mustRandomBytes(16))
}

func newToken() string {
	return base64.URLEncoding.EncodeToString(mustRandomBytes(32))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isTokenHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func persistedDeviceToken(value string) string {
	if value == "" || isTokenHash(value) {
		return value
	}
	return hashToken(value)
}

func preferencePathKey(deviceID, category, relativePath, filename string) string {
	return deviceID + ":" + category + ":" + relativePath + ":" + filename
}

// DB returns the underlying database connection.
func (s *Store) DB() *sql.DB { return s.db }

// Close flushes WAL and closes the database connection.
func (s *Store) Close() error {
	s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

const (
	minPasswordLength = 8
	maxPasswordLength = 256
	maxEmailLength    = 254
)

func validateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > maxEmailLength || !strings.Contains(email, "@") || strings.ContainsAny(email, "\x00\r\n") {
		return errors.New("invalid email")
	}
	return nil
}

func validateHostname(hostname string) error {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || len(hostname) > 255 || strings.ContainsAny(hostname, "\x00\r\n/\\") {
		return errors.New("invalid hostname")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return errors.New("password too short")
	}
	if len(password) > maxPasswordLength {
		return errors.New("password too long")
	}
	return nil
}

// CreateUser inserts a new user row.
func (s *Store) CreateUser(email, password string) (User, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return User{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash := saltedHash(password)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)", id, email, hash, now)
	if err != nil {
		// Never surface the raw SQLite constraint error (it leaks internals and
		// is an enumeration oracle).
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return User{ID: id, Email: email, PasswordHash: hash, CreatedAt: textTime(now)}, nil
}

// ErrEmailTaken is returned by CreateUser when the email already has an account.
var ErrEmailTaken = errors.New("email already registered")

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func saltedHash(password string) string {
	salt := mustRandomBytes(16)
	digest := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest))
}

func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) == 5 && parts[0] == "argon2id" && parts[1] == "v=19" {
		var memory, iterations uint32
		var threads uint8
		if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
			return false
		}
		if memory < 8*1024 || memory > 1<<20 || iterations < 1 || iterations > 10 || threads < 1 || threads > 16 {
			return false
		}
		salt, err := base64.RawStdEncoding.DecodeString(parts[3])
		if err != nil {
			return false
		}
		expected, err := base64.RawStdEncoding.DecodeString(parts[4])
		if err != nil || len(expected) == 0 {
			return false
		}
		actual := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expected)))
		return subtle.ConstantTimeCompare(actual, expected) == 1
	}

	// Legacy hashes are accepted only to allow a successful login to trigger
	// an immediate Argon2id rehash.
	if len(parts) == 3 && parts[0] == "s256" {
		salt, err := hex.DecodeString(parts[1])
		if err != nil {
			return false
		}
		expected, err := hex.DecodeString(parts[2])
		if err != nil {
			return false
		}
		actual := sha256.Sum256(append(salt, []byte(password)...))
		return subtle.ConstantTimeCompare(actual[:], expected) == 1
	}
	return false
}

// AuthenticateUser looks up a user by email and password.
func (s *Store) AuthenticateUser(email, password string) (User, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return User{}, errors.New("invalid credentials")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	var created string
	err := s.db.QueryRow("SELECT id, email, password_hash, created_at FROM users WHERE email = ?", email).Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errors.New("invalid credentials")
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = textTime(created)
	if !verifyPassword(u.PasswordHash, password) {
		return User{}, errors.New("invalid credentials")
	}
	if strings.HasPrefix(u.PasswordHash, "s256$") {
		u.PasswordHash = saltedHash(password)
		if _, err := s.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", u.PasswordHash, u.ID); err != nil {
			return User{}, err
		}
	}
	return u, nil
}

// UserByEmail returns the account with the given email. The error is
// sql.ErrNoRows when no such account exists.
func (s *Store) UserByEmail(email string) (User, error) {
	email = strings.TrimSpace(email)
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	var created string
	err := s.db.QueryRow("SELECT id, email, password_hash, created_at FROM users WHERE email = ?", email).Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = textTime(created)
	return u, nil
}

// CountUsers returns how many accounts exist. It is used to detect a locked-out
// deployment (registration disabled and no admin configured).
func (s *Store) CountUsers() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// EnsureAdminUser creates the initial admin account from environment
// configuration if it does not exist yet. An existing account is never
// modified, so a password changed in the dashboard is preserved.
func (s *Store) EnsureAdminUser(email, password string) (User, bool, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return User{}, false, err
	}
	if existing, err := s.UserByEmail(email); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return User{}, false, err
	}
	user, err := s.CreateUser(email, password)
	if errors.Is(err, ErrEmailTaken) {
		// Lost a race to a concurrent bootstrap; use the existing account.
		existing, lookupErr := s.UserByEmail(email)
		if lookupErr != nil {
			return User{}, false, lookupErr
		}
		return existing, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return user, true, nil
}

// CreateSession creates a new session for the given owner. Only a hash of the
// bearer token is persisted; the raw token is returned to the client once.
func (s *Store) CreateSession(ownerID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := newToken()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("INSERT INTO sessions (token, owner_id, created_at) VALUES (?, ?, ?)", hashToken(token), ownerID, now)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, OwnerID: ownerID, CreatedAt: textTime(now)}, nil
}

// SessionOwner returns the owner_id for a session token, or "" if expired/missing.
func (s *Store) SessionOwner(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var ownerID, created, storedToken string
	err := s.db.QueryRow("SELECT token, owner_id, created_at FROM sessions WHERE token = ?", hashToken(token)).Scan(&storedToken, &ownerID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		// Migrate sessions created before token hashing was introduced.
		err = s.db.QueryRow("SELECT token, owner_id, created_at FROM sessions WHERE token = ?", token).Scan(&storedToken, &ownerID, &created)
		if err == nil {
			_, _ = s.db.Exec("UPDATE sessions SET token = ? WHERE token = ?", hashToken(token), token)
			storedToken = hashToken(token)
		}
	}
	if err != nil {
		return "", false
	}
	if time.Since(textTime(created)) > sessionTTL {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE token = ?", storedToken)
		return "", false
	}
	return ownerID, true
}

// RevokeSession invalidates one session token.
func (s *Store) RevokeSession(token string) error {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM sessions WHERE token = ? OR token = ?", hashToken(token), token)
	return err
}

// RevokeUserSessions invalidates all sessions for an owner.
func (s *Store) RevokeUserSessions(ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM sessions WHERE owner_id = ?", ownerID)
	return err
}

// UserByID returns a user by ID.
func (s *Store) UserByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow("SELECT id, email, password_hash, created_at FROM users WHERE id = ?", id)
	var u User
	var created string
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &created); err != nil {
		return nil, err
	}
	u.CreatedAt = textTime(created)
	return &u, nil
}

// UpdateUserEmail changes the email for a user.
func (s *Store) UpdateUserEmail(userID, email string) error {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE users SET email = ? WHERE id = ?", email, userID)
	return err
}

// UpdateUserPassword changes the password for a user after verifying the current one.
func (s *Store) UpdateUserPassword(userID, currentPassword, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var hash string
	err := s.db.QueryRow("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash)
	if err != nil {
		return err
	}
	if !verifyPassword(hash, currentPassword) {
		return errors.New("invalid current password")
	}
	newHash := saltedHash(newPassword)
	_, err = s.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", newHash, userID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM sessions WHERE owner_id = ?", userID)
	return err
}

// ListDevicesForOwner returns devices for a specific owner.
func (s *Store) ListDevicesForOwner(ownerID string) ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT "+deviceColumns+" FROM devices WHERE owner_id = ?", ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []Device
	for rows.Next() {
		d, err := scanDeviceRow(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// ListDeviceSummariesForOwner returns the lightweight dashboard projection for
// a user's devices, avoiding the heavy hardware/apps JSON payloads.
func (s *Store) ListDeviceSummariesForOwner(ownerID string) ([]DeviceSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`
		SELECT d.id, d.user_id, d.owner_id, d.hostname, d.last_seen_at, d.last_sync_at,
		       d.sync_failures, d.last_error, d.last_error_at, d.status, d.created_at, d.updated_at,
		       d.hardware_fingerprint, COALESCE(d.hardware_json, ''), d.display_name,
		       COALESCE(d.tags_json, '[]'), d.collection_interval_seconds,
		       CASE WHEN d.apps_json IS NULL OR d.apps_json = '' THEN 0 ELSE json_array_length(d.apps_json) END,
		       (SELECT COUNT(*) FROM files f WHERE f.device_id = d.id AND f.category != 'saves'),
		       (SELECT COUNT(*) FROM files f WHERE f.device_id = d.id AND f.category = 'saves'),
		       (SELECT COALESCE(SUM(f.size_bytes), 0) FROM files f WHERE f.device_id = d.id AND f.category = 'saves')
		FROM devices d WHERE d.owner_id = ?`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []DeviceSummary{}
	for rows.Next() {
		var d DeviceSummary
		var lastSeen, lastSync, lastErrorAt, createdAt, updatedAt, hardware, tags string
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.OwnerID, &d.Hostname, &lastSeen, &lastSync,
			&d.SyncFailures, &d.LastError, &lastErrorAt, &d.Status, &createdAt, &updatedAt,
			&d.HardwareFingerprint, &hardware, &d.DisplayName, &tags, &d.CollectionIntervalSeconds, &d.AppCount,
			&d.PreferenceCount, &d.SavesCount, &d.SavesSizeBytes,
		); err != nil {
			return nil, err
		}
		d.LastSeenAt = textTime(lastSeen)
		if lastSync != "" {
			d.LastSyncAt = textTime(lastSync)
		}
		if lastErrorAt != "" {
			d.LastErrorAt = textTime(lastErrorAt)
		}
		d.CreatedAt = textTime(createdAt)
		d.UpdatedAt = textTime(updatedAt)
		if hardware != "" {
			_ = json.Unmarshal([]byte(hardware), &d.Hardware)
		}
		if tags != "" {
			_ = json.Unmarshal([]byte(tags), &d.Tags)
		}
		if d.Tags == nil {
			d.Tags = []string{}
		}
		summaries = append(summaries, d)
	}
	return summaries, rows.Err()
}

// RecordSyncError records a sync error on a device.
func (s *Store) RecordSyncError(deviceID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("UPDATE devices SET sync_failures = sync_failures + 1, last_error = ?, last_error_at = ?, status = 'error', updated_at = ? WHERE id = ?", errMsg, now, now, deviceID)
	return err
}

// ConsumeEnrollmentToken marks an enrollment token as used.
func (s *Store) ConsumeEnrollmentToken(token, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("UPDATE enrollment_tokens SET used_at = ?, device_id = ? WHERE token = ?", now, deviceID, token)
	return err
}

// RecordSyncTimestamp updates the last_sync_at for a device.
func (s *Store) RecordSyncTimestamp(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("UPDATE devices SET last_sync_at = ?, status = 'online', updated_at = ? WHERE id = ?", now, now, deviceID)
	return err
}

// GetTelemetryHistory returns raw telemetry points for a device.
func (s *Store) GetTelemetryHistory(deviceID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT id, payload, received_at FROM telemetry_raw WHERE device_id = ? ORDER BY id DESC LIMIT ?", deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []map[string]any
	for rows.Next() {
		var id int64
		var payload, received string
		if err := rows.Scan(&id, &payload, &received); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			m = map[string]any{"raw": payload}
		}
		m["id"] = id
		m["received_at"] = received
		points = append(points, m)
	}
	return points, rows.Err()
}

// CleanupDeviceTelemetry deletes all telemetry data for a device.
func (s *Store) CleanupDeviceTelemetry(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM telemetry_raw WHERE device_id = ?", deviceID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM telemetry_downsampled WHERE device_id = ?", deviceID)
	return err
}

// GetSyncConfig returns extra directories for a user.
func (s *Store) GetSyncConfig(ownerID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var dirsJSON string
	err := s.db.QueryRow("SELECT extra_dirs FROM sync_config WHERE owner_id = ?", ownerID).Scan(&dirsJSON)
	if err != nil {
		return []string{}, nil
	}
	var dirs []string
	if err := json.Unmarshal([]byte(dirsJSON), &dirs); err != nil {
		return []string{}, nil
	}
	return dirs, nil
}

func validateExtraDirs(dirs []string) error {
	if len(dirs) > maxExtraDirs {
		return fmt.Errorf("too many extra directories: maximum %d", maxExtraDirs)
	}
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return errors.New("extra directory cannot be empty")
		}
		if len(dir) > maxExtraDirLength {
			return fmt.Errorf("extra directory is too long: maximum %d", maxExtraDirLength)
		}
		if strings.Contains(dir, "..") {
			return errors.New("extra directory cannot contain '..'")
		}
		if strings.ContainsAny(dir, "\x00\r\n") {
			return errors.New("extra directory contains invalid characters")
		}
	}
	return nil
}

// SetSyncConfig stores extra directories for a user.
func (s *Store) SetSyncConfig(ownerID string, dirs []string) error {
	if err := validateExtraDirs(dirs); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dirsJSON, _ := json.Marshal(dirs)
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec(
		`INSERT INTO sync_config (owner_id, extra_dirs, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(owner_id) DO UPDATE SET extra_dirs = excluded.extra_dirs, updated_at = excluded.updated_at`,
		ownerID, string(dirsJSON), now,
	)
	return err
}

func (s *Store) GetWorkspaceDirs(ownerID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var dirsJSON string
	err := s.db.QueryRow("SELECT workspace_dirs FROM sync_config WHERE owner_id = ?", ownerID).Scan(&dirsJSON)
	if err != nil {
		return []string{}, nil
	}
	var dirs []string
	if err := json.Unmarshal([]byte(dirsJSON), &dirs); err != nil {
		return []string{}, nil
	}
	return dirs, nil
}

func (s *Store) SetWorkspaceDirs(ownerID string, dirs []string) error {
	if err := validateExtraDirs(dirs); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dirsJSON, _ := json.Marshal(dirs)
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec(
		`INSERT INTO sync_config (owner_id, extra_dirs, workspace_dirs, updated_at) VALUES (?, '[]', ?, ?)
		 ON CONFLICT(owner_id) DO UPDATE SET workspace_dirs = excluded.workspace_dirs, updated_at = excluded.updated_at`,
		ownerID, string(dirsJSON), now,
	)
	return err
}

// DeleteDevice removes a device.
func (s *Store) DeleteDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// device_logs has no foreign key, so the lines are removed explicitly here
	// rather than relying on cascade. Done inline because the locked helper in
	// device_logs.go would deadlock on this same mutex.
	if _, err := s.db.ExecContext(context.Background(), "DELETE FROM device_logs WHERE device_id = ?", deviceID); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM devices WHERE id = ?", deviceID)
	return err
}

func normalizeDeviceSettings(settings DeviceSettings) (DeviceSettings, string, error) {
	settings.DisplayName = strings.TrimSpace(settings.DisplayName)
	if utf8.RuneCountInString(settings.DisplayName) > 64 {
		return DeviceSettings{}, "", errors.New("display name must be at most 64 characters")
	}
	switch settings.CollectionIntervalSeconds {
	case 5, 10, 30, 60:
	default:
		return DeviceSettings{}, "", errors.New("collection interval must be 5, 10, 30, or 60 seconds")
	}
	if len(settings.Tags) > maxDeviceTags {
		return DeviceSettings{}, "", fmt.Errorf("at most %d tags are allowed", maxDeviceTags)
	}
	tags := make([]string, 0, len(settings.Tags))
	seen := make(map[string]bool, len(settings.Tags))
	for _, tag := range settings.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > 32 || strings.ContainsAny(tag, ",\r\n") {
			return DeviceSettings{}, "", errors.New("tags must be at most 32 characters and cannot contain commas or newlines")
		}
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	settings.Tags = tags
	tagsJSON, err := json.Marshal(tags)
	return settings, string(tagsJSON), err
}

func (s *Store) GetDeviceSettings(ctx context.Context, deviceID string) (DeviceSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var settings DeviceSettings
	var tagsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT display_name, COALESCE(tags_json, '[]'), collection_interval_seconds
		 FROM devices WHERE id = ?`, deviceID,
	).Scan(&settings.DisplayName, &tagsJSON, &settings.CollectionIntervalSeconds)
	if err != nil {
		return DeviceSettings{}, err
	}
	if err := json.Unmarshal([]byte(tagsJSON), &settings.Tags); err != nil {
		return DeviceSettings{}, err
	}
	if settings.Tags == nil {
		settings.Tags = []string{}
	}
	return settings, nil
}

func (s *Store) UpdateDeviceSettings(ctx context.Context, deviceID string, settings DeviceSettings) error {
	settings, tagsJSON, err := normalizeDeviceSettings(settings)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx,
		`UPDATE devices SET display_name = ?, tags_json = ?, collection_interval_seconds = ?, updated_at = ?
		 WHERE id = ?`,
		settings.DisplayName, tagsJSON, settings.CollectionIntervalSeconds, timeText(time.Now().UTC()), deviceID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("device not found")
	}
	return nil
}

func normalizeUpdateInventory(report UpdateInventory) (UpdateInventory, error) {
	switch report.Status {
	case "ready", "unsupported", "error":
	default:
		return UpdateInventory{}, errors.New("invalid update inventory status")
	}
	if len(report.Message) > 512 {
		return UpdateInventory{}, errors.New("update inventory message is too long")
	}
	if len(report.Updates) > maxPendingUpdates {
		return UpdateInventory{}, fmt.Errorf("too many pending updates: maximum %d", maxPendingUpdates)
	}
	if report.Status != "ready" {
		report.Updates = []PendingUpdate{}
	}
	for i := range report.Updates {
		update := &report.Updates[i]
		update.Name = strings.TrimSpace(update.Name)
		update.NewVersion = strings.TrimSpace(update.NewVersion)
		update.CurrentVersion = strings.TrimSpace(update.CurrentVersion)
		switch update.Source {
		case "apt", "flatpak", "pacman", "aur":
		default:
			return UpdateInventory{}, fmt.Errorf("unsupported update source %q", update.Source)
		}
		if update.Name == "" || len(update.Name) > 255 || update.NewVersion == "" || len(update.NewVersion) > 128 || len(update.CurrentVersion) > 128 {
			return UpdateInventory{}, errors.New("invalid pending update name or version")
		}
		if strings.ContainsAny(update.Name+update.CurrentVersion+update.NewVersion, "\r\n\x00") {
			return UpdateInventory{}, errors.New("invalid control character in pending update")
		}
	}
	report.CheckedAt = timeText(time.Now().UTC())
	if report.Updates == nil {
		report.Updates = []PendingUpdate{}
	}
	return report, nil
}

func (s *Store) GetPendingUpdates(ctx context.Context, deviceID string) (UpdateInventory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(updates_json, '{"status":"not_reported","updates":[]}') FROM devices WHERE id = ?`, deviceID).Scan(&raw); err != nil {
		return UpdateInventory{}, err
	}
	var report UpdateInventory
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return UpdateInventory{}, err
	}
	if report.Updates == nil {
		report.Updates = []PendingUpdate{}
	}
	return report, nil
}

func (s *Store) UpdatePendingUpdates(ctx context.Context, deviceID string, report UpdateInventory) error {
	report, err := normalizeUpdateInventory(report)
	if err != nil {
		return err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET updates_json = ?, updated_at = ? WHERE id = ?`, string(data), timeText(time.Now().UTC()), deviceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("device not found")
	}
	return nil
}

// DeviceStats returns aggregate stats.
func (s *Store) DeviceStats() (map[string]any, error) {
	return map[string]any{}, nil
}

// Cleanup deletes old data based on retention settings.
func (s *Store) Cleanup(olderThan time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec("DELETE FROM telemetry_raw WHERE received_at < ?", timeText(olderThan))
	_, _ = s.db.Exec("DELETE FROM telemetry_downsampled WHERE timestamp < ?", timeText(olderThan))
	return nil
}

// CleanupAudit deletes old audit logs.
func (s *Store) CleanupAudit(olderThan time.Time) error {
	return nil
}

// CheckpointWAL runs a passive WAL checkpoint to keep the WAL file small.
func (s *Store) CheckpointWAL() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	return err
}

// RunRetentionCleanup deletes telemetry data older than the per-owner retention settings.
// Returns the total number of deleted rows across all owners.
func (s *Store) RunRetentionCleanup() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	var totalDeleted int64

	type ownerRetention struct {
		ownerID string
		raw     int
		m1      int
		m5      int
		m1h     int
	}

	// Fetch all owners with explicit settings.
	rows, err := s.db.Query("SELECT owner_id, raw_hours, resolution_1m_days, resolution_5m_days, resolution_1h_days FROM retention_settings")
	if err != nil {
		return 0, fmt.Errorf("query retention settings: %w", err)
	}
	defer rows.Close()

	var owners []ownerRetention
	for rows.Next() {
		var or ownerRetention
		if err := rows.Scan(&or.ownerID, &or.raw, &or.m1, &or.m5, &or.m1h); err != nil {
			continue
		}
		owners = append(owners, or)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate retention settings: %w", err)
	}

	// Apply default retention for owners without explicit settings.
	ownerSet := make(map[string]bool)
	for _, o := range owners {
		ownerSet[o.ownerID] = true
	}
	devRows, err := s.db.Query("SELECT DISTINCT owner_id FROM devices")
	if err == nil {
		defer devRows.Close()
		for devRows.Next() {
			var oid string
			if err := devRows.Scan(&oid); err != nil {
				continue
			}
			if !ownerSet[oid] {
				owners = append(owners, ownerRetention{ownerID: oid, raw: 2, m1: 7, m5: 30, m1h: 365})
			}
		}
	}

	for _, o := range owners {
		// Delete expired raw telemetry for this owner's devices.
		rawCutoff := now.Add(-time.Duration(o.raw) * time.Hour)
		res, err := s.db.Exec(
			`DELETE FROM telemetry_raw WHERE device_id IN (SELECT id FROM devices WHERE owner_id = ?) AND received_at < ?`,
			o.ownerID, timeText(rawCutoff),
		)
		if err == nil {
			n, _ := res.RowsAffected()
			totalDeleted += n
		}

		// Delete expired downsampled telemetry per resolution.
		for _, tier := range []struct {
			resolution string
			days       int
		}{
			{"1m", o.m1},
			{"5m", o.m5},
			{"1h", o.m1h},
		} {
			cutoff := now.AddDate(0, 0, -tier.days)
			res, err := s.db.Exec(
				`DELETE FROM telemetry_downsampled WHERE device_id IN (SELECT id FROM devices WHERE owner_id = ?) AND resolution = ? AND timestamp < ?`,
				o.ownerID, tier.resolution, timeText(cutoff),
			)
			if err == nil {
				n, _ := res.RowsAffected()
				totalDeleted += n
			}
		}
	}

	// Clean up orphaned telemetry data for devices that no longer exist.
	res, err := s.db.Exec(`DELETE FROM telemetry_raw WHERE device_id NOT IN (SELECT id FROM devices)`)
	if err == nil {
		n, _ := res.RowsAffected()
		totalDeleted += n
	}
	res, err = s.db.Exec(`DELETE FROM telemetry_downsampled WHERE device_id NOT IN (SELECT id FROM devices)`)
	if err == nil {
		n, _ := res.RowsAffected()
		totalDeleted += n
	}

	return totalDeleted, nil
}

// QueueInstallApp queues an app install command.
func (s *Store) QueueInstallApp(deviceID, source, name string) (DeviceCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCommandLocked(deviceID, "install_app", "", name, source, "")
}

// QueueExcludeFile queues a file exclusion command.
func (s *Store) QueueExcludeFile(deviceID, relativePath string) (DeviceCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCommandLocked(deviceID, "exclude_file", relativePath, "", "", "")
}

// ListPendingCommands returns pending commands for a device.
func (s *Store) ListPendingCommands(deviceID string) []DeviceCommand {
	cmds, _ := s.GetPendingCommands(deviceID)
	return cmds
}

// GetSaveFilesForGame returns only save files belonging to the requested
// Wine prefix and game path.
func (s *Store) GetSaveFilesForGame(deviceID, prefixID, gameName string) ([]PreferenceFile, error) {
	files, err := s.ListFiles(deviceID)
	if err != nil {
		return nil, err
	}
	filtered := make([]PreferenceFile, 0)
	for _, file := range files {
		if file.Category != "saves" || !savePathInPrefix(file.RelativePath, prefixID) || !savePathInGame(file.RelativePath, gameName) {
			continue
		}
		filtered = append(filtered, file)
	}
	return filtered, nil
}

func savePathInPrefix(relativePath, prefixID string) bool {
	parts := strings.Split(strings.Trim(filepath.ToSlash(relativePath), "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "wine-prefixes" && parts[i+1] == prefixID {
			return true
		}
	}
	return false
}

func savePathInGame(relativePath, gameName string) bool {
	gameName = strings.Trim(gameName, "/")
	if gameName == "" {
		return true
	}
	pathParts := strings.Split(strings.Trim(filepath.ToSlash(relativePath), "/"), "/")
	gameParts := strings.Split(gameName, "/")
	for i := 0; i+len(gameParts) <= len(pathParts); i++ {
		match := true
		for j := range gameParts {
			if pathParts[i+j] != gameParts[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// QueueRestoreSaves keeps the original command shape for callers that do not
// attach files. The agent-facing command uses QueueRestoreSavesWithFiles.
func (s *Store) QueueRestoreSaves(deviceID, prefixID, gameName string) (DeviceCommand, error) {
	return s.QueueRestoreSavesWithFiles(deviceID, "", prefixID, gameName, nil)
}

// QueueRestoreSavesWithFiles queues a restore command carrying the selected
// save contents to the target agent.
func (s *Store) QueueRestoreSavesWithFiles(deviceID, sourceDeviceID, prefixID, gameName string, files []PreferenceFile) (DeviceCommand, error) {
	payloadFiles := make([]RestoreSaveFile, 0, len(files))
	for _, file := range files {
		payloadFiles = append(payloadFiles, RestoreSaveFile{
			Filename: file.Filename, RelativePath: file.RelativePath,
			Content: file.Content, Encoding: file.Encoding,
		})
	}
	payload, err := json.Marshal(RestoreSavesPayload{
		SourceDeviceID: sourceDeviceID, PrefixID: prefixID, GameName: gameName, Files: payloadFiles,
	})
	if err != nil {
		return DeviceCommand{}, err
	}
	if len(payload) > 16<<20 {
		return DeviceCommand{}, errors.New("restore payload exceeds 16 MiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCommandLocked(deviceID, "restore_saves", prefixID, gameName, "", string(payload))
}

// UpdateApps updates the apps inventory for a device.
func (s *Store) UpdateApps(deviceID string, apps []AppInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(apps)
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("UPDATE devices SET apps_json = ?, updated_at = ? WHERE id = ?", string(data), now, deviceID)
	return err
}

// UpdateSystemInventory replaces one or both system inventory snapshots. A nil
// section is left untouched: the agent omits a section it could not collect, and
// a transient failure must not be read as "this device has no services".
//
// The context is threaded through from the HTTP handler so an abandoned upload
// releases its database work instead of running to completion.
func (s *Store) UpdateSystemInventory(ctx context.Context, deviceID string, services *[]ServiceUnit, ports *[]OpenPort) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	if services != nil {
		data, err := json.Marshal(normalizeServices(*services))
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, "UPDATE devices SET services_json = ?, updated_at = ? WHERE id = ?", string(data), now, deviceID); err != nil {
			return err
		}
	}
	if ports != nil {
		data, err := json.Marshal(normalizePorts(*ports))
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, "UPDATE devices SET ports_json = ?, updated_at = ? WHERE id = ?", string(data), now, deviceID); err != nil {
			return err
		}
	}
	return nil
}

// GetServices returns the last services snapshot. A device that has never
// reported one yields an empty list, not an error.
func (s *Store) GetServices(ctx context.Context, deviceID string) ([]ServiceUnit, error) {
	return systemInventoryColumn(ctx, s, deviceID, "services_json", func(raw string) ([]ServiceUnit, error) {
		var units []ServiceUnit
		if raw == "" {
			return []ServiceUnit{}, nil
		}
		if err := json.Unmarshal([]byte(raw), &units); err != nil {
			return nil, err
		}
		return units, nil
	})
}

// GetPorts returns the last listening socket snapshot.
func (s *Store) GetPorts(ctx context.Context, deviceID string) ([]OpenPort, error) {
	return systemInventoryColumn(ctx, s, deviceID, "ports_json", func(raw string) ([]OpenPort, error) {
		var ports []OpenPort
		if raw == "" {
			return []OpenPort{}, nil
		}
		if err := json.Unmarshal([]byte(raw), &ports); err != nil {
			return nil, err
		}
		return ports, nil
	})
}

func systemInventoryColumn[T any](ctx context.Context, s *Store, deviceID, column string, decode func(string) ([]T, error)) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// COALESCE is required, not cosmetic: a device row that never received an
	// inventory holds NULL, and scanning NULL into a string fails outright.
	var raw string
	query := "SELECT COALESCE(" + column + ", '') FROM devices WHERE id = ?"
	if err := s.db.QueryRowContext(ctx, query, deviceID).Scan(&raw); err != nil {
		return nil, err
	}
	return decode(raw)
}

// normalizeServices enforces the server-side caps. The agent already applies
// them, but the server must not store more than the contract allows just
// because an agent lied or an old build did not know about the limit.
func normalizeServices(units []ServiceUnit) []ServiceUnit {
	filtered := make([]ServiceUnit, 0, len(units))
	seen := make(map[string]bool, len(units))
	for _, unit := range units {
		unit.Name = strings.TrimSpace(unit.Name)
		unit.Status = strings.TrimSpace(unit.Status)
		if unit.Name == "" || seen[unit.Name] {
			continue
		}
		seen[unit.Name] = true
		filtered = append(filtered, unit)
	}
	if limit := maxSystemInventoryRows; len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func normalizePorts(ports []OpenPort) []OpenPort {
	for i := range ports {
		ports[i].Protocol = strings.ToLower(strings.TrimSpace(ports[i].Protocol))
		ports[i].Local = strings.TrimSpace(ports[i].Local)
	}
	filtered := make([]OpenPort, 0, len(ports))
	for _, port := range ports {
		// A port is only meaningful with a protocol and a number in range.
		if port.Protocol == "" || port.Port <= 0 || port.Port > 65535 {
			continue
		}
		filtered = append(filtered, port)
	}
	if limit := maxSystemInventoryRows; len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// AppendTelemetry appends a raw telemetry payload.
func (s *Store) AppendTelemetry(deviceID string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("INSERT INTO telemetry_raw (device_id, payload, received_at) VALUES (?, ?, ?)", deviceID, string(payload), now)
	return err
}

// GetBestHistory returns the best available telemetry history for a time range.
func (s *Store) GetBestHistory(deviceID string, from, to time.Time) ([]TelemetryDownsampled, string, error) {
	preferred := ResolutionForInterval(from, to)
	if preferred == "raw" {
		preferred = "1m"
	}
	choices := map[string][]string{
		"1m": {"1m", "5m", "1h"},
		"5m": {"5m", "1m", "1h"},
		"1h": {"1h", "5m", "1m"},
	}
	var lastErr error
	for _, resolution := range choices[preferred] {
		points, err := s.GetDownsampledRange(deviceID, resolution, from, to)
		if err != nil {
			lastErr = err
			continue
		}
		if len(points) > 0 {
			return points, resolution, nil
		}
	}
	return []TelemetryDownsampled{}, preferred, lastErr
}

// GetTelemetryHistoryRaw returns raw telemetry points in a time range.
func (s *Store) GetTelemetryHistoryRaw(deviceID string, from, to time.Time) ([]map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT id, payload, received_at FROM telemetry_raw WHERE device_id = ? AND received_at >= ? AND received_at <= ? ORDER BY id ASC LIMIT ?",
		deviceID, timeText(from), timeText(to), maxTelemetryRows,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []map[string]any
	for rows.Next() {
		var id int64
		var payload, received string
		if err := rows.Scan(&id, &payload, &received); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			m = map[string]any{"raw": payload}
		}
		m["id"] = id
		m["received_at"] = received
		points = append(points, m)
	}
	return points, rows.Err()
}

// RemoveDevice is an alias for DeleteDevice.
func (s *Store) RemoveDevice(deviceID string) error {
	return s.DeleteDevice(deviceID)
}

// SaveExtraDirs is an alias for SetSyncConfig.
func (s *Store) SaveExtraDirs(ownerID string, dirs []string) error {
	return s.SetSyncConfig(ownerID, dirs)
}

// GetExtraDirs is an alias for GetSyncConfig.
func (s *Store) GetExtraDirs(ownerID string) ([]string, error) {
	return s.GetSyncConfig(ownerID)
}

// RetentionSettings holds data retention configuration per owner.
type RetentionSettings struct {
	OwnerID          string `json:"owner_id"`
	RawHours         int    `json:"raw_hours"`
	Resolution1mDays int    `json:"1m_days"`
	Resolution5mDays int    `json:"5m_days"`
	Resolution1hDays int    `json:"1h_days"`
}

// Rejection represents a rejected preference file (alias for PreferenceRejection).
type Rejection = PreferenceRejection

// GetRetentionSettings returns retention settings for an owner.
func (s *Store) GetRetentionSettings(ownerID string) (*RetentionSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var rs RetentionSettings
	var updatedAt string
	err := s.db.QueryRow(
		"SELECT owner_id, raw_hours, resolution_1m_days, resolution_5m_days, resolution_1h_days, updated_at FROM retention_settings WHERE owner_id = ?",
		ownerID,
	).Scan(&rs.OwnerID, &rs.RawHours, &rs.Resolution1mDays, &rs.Resolution5mDays, &rs.Resolution1hDays, &updatedAt)
	if err != nil {
		return &RetentionSettings{OwnerID: ownerID, RawHours: 2, Resolution1mDays: 7, Resolution5mDays: 30, Resolution1hDays: 365}, nil
	}
	return &rs, nil
}

func validateRetentionSettings(rs RetentionSettings) error {
	if strings.TrimSpace(rs.OwnerID) == "" {
		return errors.New("owner_id is required")
	}
	if rs.RawHours < 1 || rs.RawHours > 168 {
		return errors.New("raw_hours must be between 1 and 168")
	}
	for name, value := range map[string]int{
		"1m_days": rs.Resolution1mDays,
		"5m_days": rs.Resolution5mDays,
		"1h_days": rs.Resolution1hDays,
	} {
		if value < 1 || value > 3650 {
			return fmt.Errorf("%s must be between 1 and 3650", name)
		}
	}
	return nil
}

// UpdateRetentionSettings stores retention settings.
func (s *Store) UpdateRetentionSettings(rs RetentionSettings) error {
	if err := validateRetentionSettings(rs); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec(
		`INSERT INTO retention_settings (owner_id, raw_hours, resolution_1m_days, resolution_5m_days, resolution_1h_days, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(owner_id) DO UPDATE SET
		   raw_hours = excluded.raw_hours, resolution_1m_days = excluded.resolution_1m_days,
		   resolution_5m_days = excluded.resolution_5m_days, resolution_1h_days = excluded.resolution_1h_days,
		   updated_at = excluded.updated_at`,
		rs.OwnerID, rs.RawHours, rs.Resolution1mDays, rs.Resolution5mDays, rs.Resolution1hDays, now,
	)
	return err
}

// CleanupOldLogs deletes log entries older than the given number of days.
func (s *Store) CleanupOldLogs(days int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	result, err := s.db.Exec("DELETE FROM logs WHERE level != 'AUDIT' AND ts < ?", timeText(cutoff))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CleanupOldLogsForOwner deletes old log entries visible to one owner.
func (s *Store) CleanupOldLogsForOwner(ownerID string, days int) (int64, error) {
	if ownerID == "" {
		return 0, errors.New("owner_id is required")
	}
	if days < 1 {
		return 0, errors.New("days must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	result, err := s.db.Exec(`DELETE FROM logs
		WHERE level != 'AUDIT' AND ts < ?
		  AND (user_id = ? OR device_id IN (SELECT id FROM devices WHERE owner_id = ?))`, timeText(cutoff), ownerID, ownerID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CreateEnrollmentToken creates a new enrollment token for an owner.
func (s *Store) CreateEnrollmentToken(ownerID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := newToken()
	now := timeText(time.Now().UTC())
	expires := timeText(time.Now().UTC().Add(15 * time.Minute))
	id := "et_" + token[:12]
	_, err := s.db.Exec("INSERT INTO enrollment_tokens (id, token, owner_id, created_at, expires_at, used_at, device_id) VALUES (?, ?, ?, ?, ?, '', '')", id, token, ownerID, now, expires)
	return token, err
}

// ValidateEnrollmentToken checks if a token is valid and returns the owner_id.
func (s *Store) ValidateEnrollmentToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ownerID, expiresAt string
	err := s.db.QueryRow("SELECT owner_id, expires_at FROM enrollment_tokens WHERE token = ? AND used_at = ''", token).Scan(&ownerID, &expiresAt)
	if err != nil {
		return "", err
	}
	if expiresAt != "" {
		expiry := textTime(expiresAt)
		if time.Now().After(expiry) {
			return "", fmt.Errorf("enrollment token expired")
		}
	}
	return ownerID, nil
}

// DeviceNote is a note attached to a device.
type DeviceNote struct {
	ID        string `json:"id"`
	DeviceID  string `json:"device_id"`
	OwnerID   string `json:"owner_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// DeviceAttachment is a file attachment for a device.
type DeviceAttachment struct {
	ID        string `json:"id"`
	DeviceID  string `json:"device_id"`
	OwnerID   string `json:"owner_id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Caption   string `json:"caption,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
}

// GetDeviceNotes returns all notes for a device.
func (s *Store) GetDeviceNotes(deviceID, ownerID string) ([]DeviceNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT id, device_id, owner_id, content, created_at, updated_at FROM device_notes WHERE device_id = ? AND owner_id = ? ORDER BY created_at DESC", deviceID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []DeviceNote
	for rows.Next() {
		var n DeviceNote
		if err := rows.Scan(&n.ID, &n.DeviceID, &n.OwnerID, &n.Content, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// CreateDeviceNote creates a new note for a device.
func (s *Store) CreateDeviceNote(deviceID, ownerID, content string) (DeviceNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("INSERT INTO device_notes (id, device_id, owner_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", id, deviceID, ownerID, content, now, now)
	if err != nil {
		return DeviceNote{}, err
	}
	return DeviceNote{ID: id, DeviceID: deviceID, OwnerID: ownerID, Content: content, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateDeviceNote updates a note's content.
func (s *Store) UpdateDeviceNote(noteID, ownerID, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("UPDATE device_notes SET content = ?, updated_at = ? WHERE id = ? AND owner_id = ?", content, now, noteID, ownerID)
	return err
}

// DeleteDeviceNote deletes a note.
func (s *Store) DeleteDeviceNote(noteID, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM device_notes WHERE id = ? AND owner_id = ?", noteID, ownerID)
	return err
}

// GetDeviceAttachments returns all attachments for a device.
func (s *Store) GetDeviceAttachments(deviceID, ownerID string) ([]DeviceAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT id, device_id, owner_id, filename, mime_type, caption, size_bytes, created_at FROM device_attachments WHERE device_id = ? AND owner_id = ? ORDER BY created_at DESC", deviceID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var atts []DeviceAttachment
	for rows.Next() {
		var a DeviceAttachment
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.OwnerID, &a.Filename, &a.MimeType, &a.Caption, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

// CreateDeviceAttachment creates a new attachment.
func (s *Store) CreateDeviceAttachment(deviceID, ownerID, filename, mimeType, caption string, data []byte) (DeviceAttachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	now := timeText(time.Now().UTC())
	_, err := s.db.Exec("INSERT INTO device_attachments (id, device_id, owner_id, filename, mime_type, caption, data, size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", id, deviceID, ownerID, filename, mimeType, caption, data, len(data), now)
	if err != nil {
		return DeviceAttachment{}, err
	}
	return DeviceAttachment{ID: id, DeviceID: deviceID, OwnerID: ownerID, Filename: filename, MimeType: mimeType, Caption: caption, SizeBytes: int64(len(data)), CreatedAt: now}, nil
}

// GetDeviceAttachmentData returns an attachment and its binary data.
func (s *Store) GetDeviceAttachmentData(attID, ownerID string) (DeviceAttachment, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var a DeviceAttachment
	var data []byte
	err := s.db.QueryRow("SELECT id, device_id, owner_id, filename, mime_type, caption, size_bytes, created_at, data FROM device_attachments WHERE id = ? AND owner_id = ?", attID, ownerID).
		Scan(&a.ID, &a.DeviceID, &a.OwnerID, &a.Filename, &a.MimeType, &a.Caption, &a.SizeBytes, &a.CreatedAt, &data)
	return a, data, err
}

// UpdateDeviceAttachmentCaption updates an attachment's caption.
func (s *Store) UpdateDeviceAttachmentCaption(attID, ownerID, caption string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE device_attachments SET caption = ? WHERE id = ? AND owner_id = ?", caption, attID, ownerID)
	return err
}

// DeleteDeviceAttachment deletes an attachment.
func (s *Store) DeleteDeviceAttachment(attID, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM device_attachments WHERE id = ? AND owner_id = ?", attID, ownerID)
	return err
}

// SecurityAudit represents a stored Lynis security audit report.
type SecurityAudit struct {
	ID               string `json:"id"`
	DeviceID         string `json:"device_id"`
	OwnerID          string `json:"owner_id"`
	HardeningIndex   int    `json:"hardening_index"`
	TotalWarnings    int    `json:"total_warnings"`
	TotalSuggestions int    `json:"total_suggestions"`
	TotalTests       int    `json:"total_tests"`
	TestsPassed      int    `json:"tests_passed"`
	LynisVersion     string `json:"lynis_version"`
	OSInfo           string `json:"os_info"`
	KernelVersion    string `json:"kernel_version"`
	ReportJSON       string `json:"report_json"`
	CreatedAt        string `json:"created_at"`
}

// SaveSecurityAudit stores a Lynis audit report.
func (s *Store) SaveSecurityAudit(deviceID, ownerID, reportJSON string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type lynisReport struct {
		HardeningIndex   int    `json:"hardening_index"`
		TotalWarnings    int    `json:"total_warnings"`
		TotalSuggestions int    `json:"total_suggestions"`
		TotalTests       int    `json:"total_tests"`
		TestsPassed      int    `json:"tests_passed"`
		LynisVersion     string `json:"lynis_version"`
		OS               string `json:"os"`
		Kernel           string `json:"kernel"`
	}
	var report lynisReport
	if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
		return "", fmt.Errorf("unmarshal report: %w", err)
	}

	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO security_audits (id, device_id, owner_id, hardening_index, total_warnings, total_suggestions, total_tests, tests_passed, lynis_version, os_info, kernel_version, report_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, deviceID, ownerID, report.HardeningIndex, report.TotalWarnings, report.TotalSuggestions,
		report.TotalTests, report.TestsPassed, report.LynisVersion, report.OS, report.Kernel, reportJSON,
	)
	if err != nil {
		return "", fmt.Errorf("insert security audit: %w", err)
	}
	return id, nil
}

// GetSecurityAudits returns the most recent audits for a device.
func (s *Store) GetSecurityAudits(deviceID string, limit int) ([]SecurityAudit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, device_id, owner_id, hardening_index, total_warnings, total_suggestions, total_tests, tests_passed, lynis_version, os_info, kernel_version, report_json, created_at
		 FROM security_audits WHERE device_id = ? ORDER BY created_at DESC LIMIT ?`,
		deviceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var audits []SecurityAudit
	for rows.Next() {
		var a SecurityAudit
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.OwnerID, &a.HardeningIndex, &a.TotalWarnings, &a.TotalSuggestions, &a.TotalTests, &a.TestsPassed, &a.LynisVersion, &a.OSInfo, &a.KernelVersion, &a.ReportJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		audits = append(audits, a)
	}
	return audits, nil
}

// GetSecurityAudit returns a single audit by ID.
func (s *Store) GetSecurityAudit(auditID string) (*SecurityAudit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var a SecurityAudit
	err := s.db.QueryRow(
		`SELECT id, device_id, owner_id, hardening_index, total_warnings, total_suggestions, total_tests, tests_passed, lynis_version, os_info, kernel_version, report_json, created_at
		 FROM security_audits WHERE id = ?`,
		auditID,
	).Scan(&a.ID, &a.DeviceID, &a.OwnerID, &a.HardeningIndex, &a.TotalWarnings, &a.TotalSuggestions, &a.TotalTests, &a.TestsPassed, &a.LynisVersion, &a.OSInfo, &a.KernelVersion, &a.ReportJSON, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// DeleteSecurityAudit removes a single audit record.
func (s *Store) DeleteSecurityAudit(auditID, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM security_audits WHERE id = ? AND owner_id = ?", auditID, ownerID)
	return err
}
