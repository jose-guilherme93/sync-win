package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Config holds logger configuration.
type Config struct {
	Level         Level         // Minimum level to persist
	ConsoleLevel  Level         // Minimum level to print to stderr
	RetentionDays int           // Days to keep logs per level
	BatchSize     int           // Max entries per batch write
	FlushInterval time.Duration // How often to flush the queue
	QueueSize     int           // Max entries in memory queue
	DedupWindow   time.Duration // Deduplication window
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Level:         LevelInfo,
		ConsoleLevel:  LevelInfo, // Show INFO and above on console
		RetentionDays: 14,
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
		QueueSize:     10000,
		DedupWindow:   5 * time.Minute,
	}
}

// Logger is the core logging engine.
type Logger struct {
	config      Config
	store       *Store
	dedup       *deduplicator
	queue       chan *LogEntry
	wg          sync.WaitGroup
	stopCh      chan struct{}
	requestID   string
	correlation string
}

// New creates a new Logger. Call Stop() to flush and close.
func New(config Config, store *Store) *Logger {
	l := &Logger{
		config: config,
		store:  store,
		queue:  make(chan *LogEntry, config.QueueSize),
		stopCh: make(chan struct{}),
	}

	l.dedup = newDeduplicator(config.DedupWindow, 1000, func(entry *dedupEntry) {
		// Flush aggregated dedup entry as a single log
		l.writeToStore(&LogEntry{
			Timestamp: entry.LastSeen,
			Level:     entry.Level,
			Category:  entry.Category,
			Event:     entry.Event,
			DeviceID:  entry.DeviceID,
			Message:   fmt.Sprintf("suppressed %d similar events (first: %s)", entry.Count, entry.Message),
			Metadata:  map[string]any{"suppressed_count": entry.Count, "first_seen": entry.FirstSeen},
		})
	})

	// Start batch writer
	l.wg.Add(1)
	go l.batchWriter()

	return l
}

// Stop flushes remaining logs and closes the store.
func (l *Logger) Stop() {
	close(l.stopCh)
	l.wg.Wait()
	// Final flush
	l.dedup.stop()
	l.flushQueue()
}

// SetRequestContext sets request_id and correlation_id for the current goroutine.
// For HTTP middleware, use WithRequestContext instead.
func (l *Logger) SetRequestContext(requestID, correlationID string) {
	l.requestID = requestID
	l.correlation = correlationID
}

// ClearRequestContext clears the request context.
func (l *Logger) ClearRequestContext() {
	l.requestID = ""
	l.correlation = ""
}

// Log logs a structured event.
func (l *Logger) Log(level Level, category Category, event Event, msg string, metadata map[string]any) {
	if level < l.config.Level && level != LevelAudit {
		return
	}

	// Redact sensitive data
	redacted := false
	if metadata != nil {
		metadata, redacted = redactMetadata(metadata)
	}

	entry := &LogEntry{
		Timestamp:     time.Now().UTC(),
		Level:         level,
		Category:      category,
		Event:         event,
		Message:       msg,
		RequestID:     l.requestID,
		CorrelationID: l.correlation,
		Metadata:      metadata,
		Redacted:      redacted,
	}

	// Console output for important events
	if level >= l.config.ConsoleLevel || level == LevelFatal {
		l.writeConsole(entry)
	}

	// Fatal also writes to stderr
	if level == LevelFatal {
		l.writeStderr(entry)
	}

	// Check deduplication
	if !l.dedup.check(level, category, event, entry.DeviceID, msg) {
		return
	}

	// Queue for persistence
	if level >= l.config.Level || level == LevelAudit {
		select {
		case l.queue <- entry:
		default:
			// Queue full - apply backpressure
			if level >= LevelWarn || level == LevelAudit {
				// Critical: try harder
				select {
				case l.queue <- entry:
				default:
					// Last resort: write directly
					l.writeToStore(entry)
				}
			}
			// DEBUG/INFO dropped silently
		}
	}
}

// Convenience methods

func (l *Logger) Trace(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelTrace, cat, event, msg, optMeta(meta))
}

func (l *Logger) Debug(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelDebug, cat, event, msg, optMeta(meta))
}

func (l *Logger) Info(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelInfo, cat, event, msg, optMeta(meta))
}

func (l *Logger) Notice(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelNotice, cat, event, msg, optMeta(meta))
}

func (l *Logger) Warn(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelWarn, cat, event, msg, optMeta(meta))
}

func (l *Logger) Error(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelError, cat, event, msg, optMeta(meta))
}

func (l *Logger) Fatal(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelFatal, cat, event, msg, optMeta(meta))
	os.Exit(1)
}

func (l *Logger) Audit(cat Category, event Event, msg string, meta ...map[string]any) {
	l.Log(LevelAudit, cat, event, msg, optMeta(meta))
}

// WithDevice returns a logger pre-filled with device_id.
func (l *Logger) WithDevice(deviceID string) *DeviceLogger {
	return &DeviceLogger{logger: l, deviceID: deviceID}
}

func optMeta(meta []map[string]any) map[string]any {
	if len(meta) > 0 {
		return meta[0]
	}
	return nil
}

// batchWriter runs in a goroutine and flushes the queue periodically.
func (l *Logger) batchWriter() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.flushQueue()
		}
	}
}

func (l *Logger) flushQueue() {
	batch := make([]*LogEntry, 0, l.config.BatchSize)
	for len(batch) < l.config.BatchSize {
		select {
		case entry := <-l.queue:
			batch = append(batch, entry)
		default:
			goto done
		}
	}
done:
	if len(batch) > 0 {
		l.writeBatch(batch)
	}
}

func (l *Logger) writeBatch(entries []*LogEntry) {
	if l.store == nil {
		return
	}
	if err := l.store.WriteBatch(entries); err != nil {
		// Logger failure: write to stderr, never crash
		fmt.Fprintf(os.Stderr, "[LOG] batch write failed: %v\n", err)
	}
}

func (l *Logger) writeToStore(entry *LogEntry) {
	if l.store == nil {
		return
	}
	if err := l.store.WriteSingle(entry); err != nil {
		fmt.Fprintf(os.Stderr, "[LOG] write failed: %v\n", err)
	}
}

func (l *Logger) writeConsole(entry *LogEntry) {
	msg := formatConsole(entry)
	fmt.Fprintln(os.Stdout, msg)
}

func (l *Logger) writeStderr(entry *LogEntry) {
	msg := formatConsole(entry)
	fmt.Fprintln(os.Stderr, msg)
}

func formatConsole(entry *LogEntry) string {
	ts := entry.Timestamp.Format("15:04:05")
	level := entry.Level.String()
	if len(level) < 5 {
		level = level + " "
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%s %s %s %s", ts, level, entry.Category, entry.Event))

	if entry.DeviceID != "" {
		parts = append(parts, fmt.Sprintf("device=%s", entry.DeviceID))
	}

	if entry.Metadata != nil {
		if method, ok := entry.Metadata["method"].(string); ok {
			if path, ok := entry.Metadata["path"].(string); ok {
				status := entry.Metadata["status"]
				parts = append(parts, fmt.Sprintf("%s %s %v", method, path, status))
			}
		}
	}

	if entry.Message != "" {
		parts = append(parts, entry.Message)
	}
	if entry.DurationMs > 0 {
		parts = append(parts, fmt.Sprintf("duration=%dms", entry.DurationMs))
	}

	return strings.Join(parts, " ")
}

// DeviceLogger is a logger pre-filled with a device_id.
type DeviceLogger struct {
	logger   *Logger
	deviceID string
}

func (d *DeviceLogger) Log(level Level, cat Category, event Event, msg string, meta ...map[string]any) {
	m := optMeta(meta)
	if m == nil {
		m = make(map[string]any)
	}
	m["device_id"] = d.deviceID
	d.logger.Log(level, cat, event, msg, m)
}

func (d *DeviceLogger) Info(cat Category, event Event, msg string, meta ...map[string]any) {
	d.Log(LevelInfo, cat, event, msg, meta...)
}

func (d *DeviceLogger) Warn(cat Category, event Event, msg string, meta ...map[string]any) {
	d.Log(LevelWarn, cat, event, msg, meta...)
}

func (d *DeviceLogger) Error(cat Category, event Event, msg string, meta ...map[string]any) {
	d.Log(LevelError, cat, event, msg, meta...)
}

// ParseLevelFromEnv reads LOG_LEVEL from environment.
func ParseLevelFromEnv() Level {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		return ParseLevel(strings.ToUpper(v))
	}
	return LevelInfo
}

// ParseConsoleLevelFromEnv reads LOG_CONSOLE_LEVEL from environment.
func ParseConsoleLevelFromEnv() Level {
	if v := os.Getenv("LOG_CONSOLE_LEVEL"); v != "" {
		return ParseLevel(strings.ToUpper(v))
	}
	return LevelWarn
}

// MarshalJSON implements json.Marshaler for LogEntry.
func (e *LogEntry) MarshalJSON() ([]byte, error) {
	type Alias LogEntry
	return json.Marshal(&struct {
		Level string `json:"level"`
		*Alias
	}{
		Level: e.Level.String(),
		Alias: (*Alias)(e),
	})
}
