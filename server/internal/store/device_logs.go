package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Device log lines are journal samples the agent uploads with telemetry. They
// used to live only inside the hardware_json blob, which meant the dashboard
// showed whatever the agent happened to ship on its last telemetry cycle and
// nothing else. They get their own table so a device keeps a queryable history.

const (
	// deviceLogMaxBatch bounds one ingest call. The agent samples 150 journal
	// lines, so this leaves headroom without letting a hostile or broken agent
	// push an unbounded batch.
	deviceLogMaxBatch = 1000
	// deviceLogMaxMessageBytes caps a stored line, matching the agent-side cap.
	// The unique dedupe key includes the message, so an uncapped line would
	// also bloat that index.
	deviceLogMaxMessageBytes = 2000
)

// DeviceLogRetentionDays is how far back device logs are kept. Journal samples
// are diagnostic, not an audit trail, so this is deliberately short compared to
// the server log retention. Exported because the server runs the cleanup on its
// own schedule and must not disagree with the store.
const DeviceLogRetentionDays = 7

// DeviceLogEntry is one stored journal line.
type DeviceLogEntry struct {
	ID       int64  `json:"id"`
	DeviceID string `json:"device_id"`
	TS       string `json:"ts"`
	Level    string `json:"level"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// DeviceLogQuery describes a filtered device log listing.
type DeviceLogQuery struct {
	// Level is a single severity filter. Empty means every level.
	Level string
	// Source is an exact service/unit match. Empty means every source.
	Source string
	// Search is a case-insensitive substring match on the message.
	Search string
	// Since, when set, drops anything older than this instant.
	Since  time.Time
	Limit  int
	Offset int
}

// DeviceLogPage is a page of entries plus the counts the toolbar renders. The
// counts come back with the page so the level chips do not need a second
// request on every filter change.
type DeviceLogPage struct {
	Entries   []DeviceLogEntry `json:"entries"`
	Total     int              `json:"total"`
	Counts    map[string]int   `json:"counts"`
	Sources   []string         `json:"sources"`
	Limit     int              `json:"limit"`
	Offset    int              `json:"offset"`
	Truncated bool             `json:"truncated"`
	// Status is the agent's last explanation for reporting no logs, or empty
	// when it reported none successfully. A non-empty value means the Logs
	// screen should say why it is empty rather than showing a blank list.
	Status string `json:"status,omitempty"`
}

// AppendDeviceLogs ingests a batch of agent-reported lines.
//
// The agent re-ships an overlapping journal window on every sample, so the same
// line arrives repeatedly. Inserts therefore use INSERT OR IGNORE against the
// unique (device_id, ts, source, message) index: re-reporting a window is a
// no-op, and only genuinely new lines are stored. Returns the number of rows
// actually inserted.
func (s *Store) AppendDeviceLogs(deviceID, ownerID string, logs []DeviceLog) (int, error) {
	if deviceID == "" {
		return 0, fmt.Errorf("device_id is required")
	}
	if len(logs) == 0 {
		return 0, nil
	}
	if len(logs) > deviceLogMaxBatch {
		logs = logs[:deviceLogMaxBatch]
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO device_logs (device_id, owner_id, ts, level, source, message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, err
	}
	defer func() { _ = stmt.Close() }()

	inserted := 0
	for _, entry := range logs {
		ts := normalizeDeviceLogTS(entry.Timestamp)
		if ts == "" {
			// Without a usable timestamp the line cannot be ordered, deduplicated
			// or aged out. Drop it rather than storing something unqueryable.
			continue
		}
		res, err := stmt.ExecContext(ctx,
			deviceID, ownerID, ts,
			normalizeDeviceLogLevel(entry.Level),
			strings.TrimSpace(entry.Source),
			truncateBytes(entry.Message, deviceLogMaxMessageBytes),
		)
		if err != nil {
			return inserted, err
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return inserted, err
	}
	return inserted, nil
}

// ListDeviceLogs returns one page of a device's log history, newest first,
// together with the level counts and source list for the current filters.
func (s *Store) ListDeviceLogs(deviceID string, q DeviceLogQuery) (DeviceLogPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > deviceLogMaxBatch {
		limit = deviceLogMaxBatch
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := deviceLogWhere(deviceID, q)

	page := DeviceLogPage{
		Entries: []DeviceLogEntry{},
		Counts:  map[string]int{"error": 0, "warn": 0, "info": 0},
		Sources: []string{},
		Limit:   limit,
		Offset:  offset,
	}

	var total int
	if scanErr := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_logs WHERE `+where, args...).Scan(&total); scanErr != nil {
		return page, scanErr
	}
	page.Total = total
	page.Truncated = offset+limit < total

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, device_id, ts, level, source, message FROM device_logs
		 WHERE `+where+`
		 ORDER BY ts DESC, id DESC
		 LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), limit, offset)...,
	)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e DeviceLogEntry
		if scanErr := rows.Scan(&e.ID, &e.DeviceID, &e.TS, &e.Level, &e.Source, &e.Message); scanErr != nil {
			return page, scanErr
		}
		page.Entries = append(page.Entries, e)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return page, rowsErr
	}

	// The agent's explanation travels with the page so an empty list is never
	// ambiguous. Read from the latest telemetry sample rather than the log table,
	// which holds nothing when collection is failing.
	if device, deviceErr := s.getDeviceLocked(deviceID); deviceErr == nil {
		page.Status = device.Hardware.LogsStatus
	}

	// Counts ignore the level filter on purpose: the toolbar shows how many
	// entries each level would yield, not how many match the active one.
	countWhere, countArgs := deviceLogWhere(deviceID, DeviceLogQuery{Source: q.Source, Search: q.Search, Since: q.Since})
	countRows, err := s.db.QueryContext(ctx, `SELECT level, COUNT(*) FROM device_logs WHERE `+countWhere+` GROUP BY level`, countArgs...)
	if err != nil {
		return page, err
	}
	defer func() { _ = countRows.Close() }()
	for countRows.Next() {
		var level string
		var n int
		if scanErr := countRows.Scan(&level, &n); scanErr != nil {
			return page, scanErr
		}
		page.Counts[normalizeDeviceLogLevel(level)] += n
	}
	if countsErr := countRows.Err(); countsErr != nil {
		return page, countsErr
	}

	sourceWhere, sourceArgs := deviceLogWhere(deviceID, DeviceLogQuery{Since: q.Since})
	sourceRows, err := s.db.QueryContext(ctx,
		`SELECT source, COUNT(*) FROM device_logs WHERE `+sourceWhere+`
		 GROUP BY source ORDER BY COUNT(*) DESC, source ASC LIMIT 200`,
		sourceArgs...,
	)
	if err != nil {
		return page, err
	}
	defer func() { _ = sourceRows.Close() }()
	for sourceRows.Next() {
		var source string
		var n int
		if scanErr := sourceRows.Scan(&source, &n); scanErr != nil {
			return page, scanErr
		}
		page.Sources = append(page.Sources, source)
	}
	return page, sourceRows.Err()
}

// CleanupOldDeviceLogs deletes device log lines older than the retention
// window. Without this the table grows forever, since ingestion is otherwise
// append-only.
func (s *Store) CleanupOldDeviceLogs(days int) (int64, error) {
	if days < 1 {
		return 0, fmt.Errorf("days must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := timeText(time.Now().UTC().AddDate(0, 0, -days))
	result, err := s.db.ExecContext(context.Background(), "DELETE FROM device_logs WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteDeviceLogsForDevice removes every stored line for a device. Used when a
// device is deleted so orphan lines cannot linger.
func (s *Store) DeleteDeviceLogsForDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(context.Background(), "DELETE FROM device_logs WHERE device_id = ?", deviceID)
	return err
}

// deviceLogWhere builds the shared predicate for a device log query.
func deviceLogWhere(deviceID string, q DeviceLogQuery) (string, []any) {
	clauses := []string{"device_id = ?"}
	args := []any{deviceID}
	// The raw value is checked for emptiness before normalising. An unset filter
	// means "every level"; normalising "" would fold it onto "info" and silently
	// hide every other severity.
	if raw := strings.ToLower(strings.TrimSpace(q.Level)); raw != "" && raw != "all" {
		clauses = append(clauses, "level = ?")
		args = append(args, normalizeDeviceLogLevel(raw))
	}
	if source := strings.TrimSpace(q.Source); source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, source)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		// LIKE wildcards in user input must not act as wildcards.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		clauses = append(clauses, `message LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escaped+"%")
	}
	if !q.Since.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, timeText(q.Since))
	}
	return strings.Join(clauses, " AND "), args
}

// normalizeDeviceLogTS accepts the shapes an agent can report and returns
// RFC3339 UTC, which is what every time filter compares against. The agent sends
// journalctl short-iso timestamps (2026-10-07T13:03:15-03:00), which Date.parse
// in the browser and time.Parse with RFC3339 both reject because the offset has
// no colon. Empty means undatable and the caller must drop the entry.
func normalizeDeviceLogTS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.999999-0700", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return timeText(t)
		}
	}
	return ""
}

// normalizeDeviceLogLevel folds a free-form severity onto the three levels the
// dashboard renders. journald priority words and the agent's own guesses both
// arrive here, so an unrecognized level is treated as info rather than dropped.
func normalizeDeviceLogLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "err", "error", "crit", "critical", "fatal", "panic", "emerg", "alert":
		return "error"
	case "warn", "warning":
		return "warn"
	case "debug", "trace", "info", "information", "notice":
		return "info"
	default:
		return "info"
	}
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
