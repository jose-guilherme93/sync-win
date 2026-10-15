package logging

import (
	"fmt"
	"strings"
	"time"
)

// Level represents the severity of a log event.
type Level int

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelNotice
	LevelWarn
	LevelError
	LevelFatal
	LevelAudit // Always persisted, never filtered
)

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelNotice:
		return "NOTICE"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	case LevelAudit:
		return "AUDIT"
	default:
		return "UNKNOWN"
	}
}

func ParseLevel(s string) Level {
	switch s {
	case "TRACE":
		return LevelTrace
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "NOTICE":
		return LevelNotice
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	case "AUDIT":
		return LevelAudit
	default:
		return LevelInfo
	}
}

// ParseLevelStrict parses s and reports whether it was a recognized, settable
// level. Unlike ParseLevel it never falls back to INFO, so a typo in a settings
// request is a validation error instead of a silent reset. AUDIT is excluded on
// purpose: it is an always-persisted marker level, not a threshold, and FATAL
// terminates the process, so neither makes sense as the persistence level.
func ParseLevelStrict(s string) (Level, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE":
		return LevelTrace, true
	case "DEBUG":
		return LevelDebug, true
	case "INFO":
		return LevelInfo, true
	case "NOTICE":
		return LevelNotice, true
	case "WARN":
		return LevelWarn, true
	case "ERROR":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

// SettableLevels lists the thresholds the settings screen offers, in ascending
// order. It is the single source of truth for the frontend's level picker.
func SettableLevels() []string {
	return []string{"TRACE", "DEBUG", "INFO", "NOTICE", "WARN", "ERROR"}
}

// Categories lists every category the logger emits, so the settings screen can
// offer the same filter values the backend understands.
func Categories() []string {
	return []string{
		string(CatSystem), string(CatHTTP), string(CatApplication), string(CatAgent),
		string(CatDevice), string(CatAuth), string(CatSecurity), string(CatAudit),
		string(CatDatabase), string(CatSync), string(CatCommand), string(CatInstallation),
		string(CatPerformance),
	}
}

// Settings is the operator-tunable, application-wide logging configuration. It
// is persisted so a change survives a restart.
type Settings struct {
	Level         string `json:"level"`
	ConsoleLevel  string `json:"console_level"`
	RetentionDays int    `json:"retention_days"`
}

// DefaultSettings matches DefaultConfig and is written on first boot so the
// operator always sees the values actually in effect rather than an empty form.
func DefaultSettings() Settings {
	return Settings{Level: "INFO", ConsoleLevel: "WARN", RetentionDays: 14}
}

// Validate rejects a level that is not settable and a retention window outside
// the range the UI advertises.
func (s Settings) Validate() error {
	if _, ok := ParseLevelStrict(s.Level); !ok {
		return fmt.Errorf("invalid level %q", s.Level)
	}
	if _, ok := ParseLevelStrict(s.ConsoleLevel); !ok {
		return fmt.Errorf("invalid console level %q", s.ConsoleLevel)
	}
	if s.RetentionDays < 1 || s.RetentionDays > 3650 {
		return fmt.Errorf("retention_days must be between 1 and 3650")
	}
	return nil
}

// Category classifies the source of a log event.
type Category string

const (
	CatSystem       Category = "system"
	CatHTTP         Category = "http"
	CatApplication  Category = "application"
	CatAgent        Category = "agent"
	CatDevice       Category = "device"
	CatAuth         Category = "auth"
	CatSecurity     Category = "security"
	CatAudit        Category = "audit"
	CatDatabase     Category = "database"
	CatSync         Category = "sync"
	CatCommand      Category = "command"
	CatInstallation Category = "installation"
	CatPerformance  Category = "performance"
)

// Event represents a structured event type.
type Event string

// System events
const (
	EventAppStarted     Event = "application_started"
	EventAppShutdown    Event = "application_shutdown"
	EventAppError       Event = "application_error"
	EventDBError        Event = "database_error"
	EventDBMigration    Event = "database_migration"
	EventServiceRestart Event = "service_restart"

	// EventSystemInventory is logged when an agent reports its systemd unit and
	// listening port snapshots.
	EventSystemInventory Event = "system_inventory_updated"
)

// Auth events
const (
	EventAuthSuccess Event = "authentication_success"
	EventAuthFailed  Event = "authentication_failed"
	EventAuthDeny    Event = "authorization_denied"
)

// Device events
const (
	EventDeviceRegistered Event = "device_registered"
	EventDeviceOnline     Event = "device_online"
	EventDeviceOffline    Event = "device_offline"
	EventDeviceRemoved    Event = "device_removed"
	EventDeviceUpdated    Event = "device_updated"
)

// Agent events
const (
	EventAgentEnrollStart    Event = "agent_enrollment_started"
	EventAgentEnrollComplete Event = "agent_enrollment_completed"
	EventAgentEnrollFailed   Event = "agent_enrollment_failed"
	EventAgentConnectionLost Event = "agent_connection_lost"
	EventAgentUpdated        Event = "agent_updated"
	EventAgentReconnect      Event = "agent_reconnected"
)

// Command events
const (
	EventCmdCreated    Event = "command_created"
	EventCmdDispatched Event = "command_dispatched"
	EventCmdStarted    Event = "command_started"
	EventCmdCompleted  Event = "command_completed"
	EventCmdFailed     Event = "command_failed"
	EventCmdTimeout    Event = "command_timeout"
	EventCmdRejected   Event = "command_rejected"
	EventCommandResult Event = "command_result"
)

// Sync events
const (
	EventSyncStarted   Event = "sync_started"
	EventSyncCompleted Event = "sync_completed"
	EventSyncFailed    Event = "sync_failed"
	EventSyncError     Event = "sync_error"
	EventSyncComplete  Event = "sync_complete"
)

// Notification events
const (
	EventNotifyTest     Event = "notification_test"
	EventNotifyEmit     Event = "notification_emit"
	EventNotifyDeliver  Event = "notification_deliver"
	EventNotifyThrottle Event = "notification_throttle"
	EventStatusWatch    Event = "status_watcher"
)

// HTTP events (for non-standard occurrences)
const (
	EventHTTPSlowRequest Event = "http_slow_request"
	EventHTTPError       Event = "http_error"
)

// LogEntry is a single structured log record.
type LogEntry struct {
	ID            int64          `json:"id,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	Level         Level          `json:"level"`
	Category      Category       `json:"category"`
	Event         Event          `json:"event"`
	Message       string         `json:"message,omitempty"`
	DeviceID      string         `json:"device_id,omitempty"`
	UserID        string         `json:"user_id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	DurationMs    int64          `json:"duration_ms,omitempty"`
	Status        int            `json:"status,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Redacted      bool           `json:"redacted,omitempty"`
}

// HTTPAccessEntry is an aggregated HTTP access record.
type HTTPAccessEntry struct {
	ID            int64   `json:"id"`
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	StatusClass   string  `json:"status_class"`
	Count         int     `json:"count"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	MaxDurationMs int64   `json:"max_duration_ms"`
	WindowStart   string  `json:"window_start"`
	WindowEnd     string  `json:"window_end"`
}

// MetricsEntry is an application metric.
type MetricsEntry struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// shouldPersist determines if a log entry should be written to SQLite.
func shouldPersist(level Level, category Category, event Event) bool {
	// Audit and Fatal always persist
	if level == LevelAudit || level == LevelFatal {
		return true
	}
	// Errors and Warns always persist
	if level >= LevelError || level == LevelWarn {
		return true
	}
	// Notices persist
	if level == LevelNotice {
		return true
	}
	return false
}

// shouldPersistHTTP determines if an HTTP request should be logged as an event.
func shouldPersistHTTP(method string, path string, status int, durationMs int64) bool {
	// Always persist errors
	if status >= 500 {
		return true
	}
	// Persist 4xx except 404 on GET
	if status >= 400 && status < 500 {
		if status == 404 && method == "GET" {
			return false
		}
		return true
	}
	// Persist slow requests (> 1s)
	if durationMs > 1000 {
		return true
	}
	// Persist state-changing operations
	if method == "POST" || method == "PUT" || method == "DELETE" {
		return true
	}
	return false
}

// statusClass returns "2xx", "3xx", "4xx", "5xx" for an HTTP status code.
func statusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "other"
	}
}

// isNoisePath returns true for paths that generate high-frequency noise.
func isNoisePath(path string) bool {
	switch {
	case path == "/health":
		return true
	case path == "/api/agent/version":
		return true
	case path == "/api/agent/checksums":
		return true
	default:
		// Telemetry polling and heartbeat
		for _, suffix := range []string{"/telemetry", "/heartbeat", "/commands"} {
			if len(path) > len(suffix) && path[len(path)-len(suffix):] == suffix {
				return true
			}
		}
	}
	return false
}

// isStreamingPath returns true for long-lived streaming endpoints. These stay
// open for as long as the client is connected, so their duration says nothing
// about server performance. Timing the notification SSE stream produced a
// permanent http_slow_request entry with a duration in the millions of
// milliseconds, which buried the genuine slow requests.
func isStreamingPath(path string) bool {
	return strings.HasPrefix(path, "/api/notifications/stream")
}
