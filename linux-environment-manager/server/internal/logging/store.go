package logging

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store handles persistence of log entries to SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a new log store. The database must already be open.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// WriteBatch writes multiple entries in a single transaction.
func (s *Store) WriteBatch(entries []*LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO logs (ts, level, category, event, message, device_id, user_id, request_id, correlation_id, duration_ms, status, metadata, redacted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		metaJSON := marshalMetadata(entry.Metadata)
		redacted := 0
		if entry.Redacted {
			redacted = 1
		}
		if _, err := stmt.Exec(
			entry.Timestamp.Format(time.RFC3339Nano),
			entry.Level.String(),
			string(entry.Category),
			string(entry.Event),
			entry.Message,
			entry.DeviceID,
			entry.UserID,
			entry.RequestID,
			entry.CorrelationID,
			entry.DurationMs,
			entry.Status,
			metaJSON,
			redacted,
		); err != nil {
			return fmt.Errorf("insert log: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// WriteSingle writes a single entry.
func (s *Store) WriteSingle(entry *LogEntry) error {
	return s.WriteBatch([]*LogEntry{entry})
}

// QueryParams defines filters for log queries.
type QueryParams struct {
	Level         string
	Category      string
	Event         string
	DeviceID      string
	UserID        string
	RequestID     string
	CorrelationID string
	From          string
	To            string
	Search        string
	Limit         int
	Offset        int
}

// QueryResult holds paginated log results.
type QueryResult struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
	Limit   int        `json:"limit"`
	Offset  int        `json:"offset"`
}

// Query searches logs with filters.
func (s *Store) Query(params QueryParams) (*QueryResult, error) {
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 100
	}

	where, args := buildWhereClause(params)

	// Count total
	countQuery := "SELECT COUNT(*) FROM logs" + where
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count logs: %w", err)
	}

	// Fetch entries
	query := `SELECT id, ts, level, category, event, message, device_id, user_id, request_id, correlation_id, duration_ms, status, metadata, redacted
		FROM logs` + where + ` ORDER BY ts DESC LIMIT ? OFFSET ?`
	args = append(args, params.Limit, params.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	entries := make([]LogEntry, 0)
	for rows.Next() {
		var entry LogEntry
		var ts, level, category, event, metaJSON string
		var redacted int
		if err := rows.Scan(
			&entry.ID, &ts, &level, &category, &event, &entry.Message,
			&entry.DeviceID, &entry.UserID, &entry.RequestID, &entry.CorrelationID,
			&entry.DurationMs, &entry.Status, &metaJSON, &redacted,
		); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		entry.Level = ParseLevel(level)
		entry.Category = Category(category)
		entry.Event = Event(event)
		entry.Redacted = redacted == 1
		if metaJSON != "" && metaJSON != "{}" {
			_ = json.Unmarshal([]byte(metaJSON), &entry.Metadata)
		}
		entries = append(entries, entry)
	}

	return &QueryResult{
		Entries: entries,
		Total:   total,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}, nil
}

// GetByID returns a single log entry.
func (s *Store) GetByID(id int64) (*LogEntry, error) {
	query := `SELECT id, ts, level, category, event, message, device_id, user_id, request_id, correlation_id, duration_ms, status, metadata, redacted
		FROM logs WHERE id = ?`

	var entry LogEntry
	var ts, level, category, event, metaJSON string
	var redacted int
	if err := s.db.QueryRow(query, id).Scan(
		&entry.ID, &ts, &level, &category, &event, &entry.Message,
		&entry.DeviceID, &entry.UserID, &entry.RequestID, &entry.CorrelationID,
		&entry.DurationMs, &entry.Status, &metaJSON, &redacted,
	); err != nil {
		return nil, err
	}
	entry.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
	entry.Level = ParseLevel(level)
	entry.Category = Category(category)
	entry.Event = Event(event)
	entry.Redacted = redacted == 1
	if metaJSON != "" && metaJSON != "{}" {
		_ = json.Unmarshal([]byte(metaJSON), &entry.Metadata)
	}
	return &entry, nil
}

// Stats returns aggregate log statistics.
func (s *Store) Stats() (map[string]any, error) {
	stats := make(map[string]any)

	// Total count
	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&total)
	stats["total"] = total

	// By level
	levelRows, err := s.db.Query("SELECT level, COUNT(*) FROM logs GROUP BY level")
	if err == nil {
		defer levelRows.Close()
		byLevel := make(map[string]int)
		for levelRows.Next() {
			var level string
			var count int
			levelRows.Scan(&level, &count)
			byLevel[level] = count
		}
		stats["by_level"] = byLevel
	}

	// By category
	catRows, err := s.db.Query("SELECT category, COUNT(*) FROM logs GROUP BY category")
	if err == nil {
		defer catRows.Close()
		byCat := make(map[string]int)
		for catRows.Next() {
			var cat string
			var count int
			catRows.Scan(&cat, &count)
			byCat[cat] = count
		}
		stats["by_category"] = byCat
	}

	// Errors in last 24h
	var errCount24h int
	s.db.QueryRow("SELECT COUNT(*) FROM logs WHERE level IN ('ERROR', 'FATAL') AND ts > datetime('now', '-1 day')").Scan(&errCount24h)
	stats["errors_24h"] = errCount24h

	// Dedup stats
	stats["dedup"] = s.dedupStats()

	return stats, nil
}

func (s *Store) dedupStats() map[string]any {
	return map[string]any{} // Placeholder - will be filled by logger
}

// Cleanup removes logs older than retention period.
func (s *Store) Cleanup(retentionDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)

	result, err := s.db.Exec("DELETE FROM logs WHERE ts < ? AND level NOT IN ('AUDIT', 'FATAL')", cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup logs: %w", err)
	}
	return result.RowsAffected()
}

// CleanupAudit removes audit logs older than specified days.
func (s *Store) CleanupAudit(days int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)

	result, err := s.db.Exec("DELETE FROM logs WHERE ts < ? AND level = 'AUDIT'", cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup audit: %w", err)
	}
	return result.RowsAffected()
}

// GetDeviceLogs returns logs for a specific device.
func (s *Store) GetDeviceLogs(deviceID string, limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.db.Query(`SELECT id, ts, level, category, event, message, device_id, user_id, request_id, correlation_id, duration_ms, status, metadata, redacted
		FROM logs WHERE device_id = ? ORDER BY ts DESC LIMIT ?`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanLogs(rows)
}

// GetAuditTrail returns audit events.
func (s *Store) GetAuditTrail(limit int, offset int) (*QueryResult, error) {
	return s.Query(QueryParams{
		Level:  "AUDIT",
		Limit:  limit,
		Offset: offset,
	})
}

// GetErrors returns error and fatal events.
func (s *Store) GetErrors(limit int, offset int) (*QueryResult, error) {
	return s.Query(QueryParams{
		Level:  "ERROR",
		Limit:  limit,
		Offset: offset,
	})
}

func scanLogs(rows *sql.Rows) ([]LogEntry, error) {
	entries := make([]LogEntry, 0)
	for rows.Next() {
		var entry LogEntry
		var ts, level, category, event, metaJSON string
		var redacted int
		if err := rows.Scan(
			&entry.ID, &ts, &level, &category, &event, &entry.Message,
			&entry.DeviceID, &entry.UserID, &entry.RequestID, &entry.CorrelationID,
			&entry.DurationMs, &entry.Status, &metaJSON, &redacted,
		); err != nil {
			return nil, err
		}
		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		entry.Level = ParseLevel(level)
		entry.Category = Category(category)
		entry.Event = Event(event)
		entry.Redacted = redacted == 1
		if metaJSON != "" && metaJSON != "{}" {
			_ = json.Unmarshal([]byte(metaJSON), &entry.Metadata)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func buildWhereClause(params QueryParams) (string, []any) {
	var conditions []string
	var args []any

	if params.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, params.Level)
	}
	if params.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, params.Category)
	}
	if params.Event != "" {
		conditions = append(conditions, "event = ?")
		args = append(args, params.Event)
	}
	if params.DeviceID != "" {
		conditions = append(conditions, "device_id = ?")
		args = append(args, params.DeviceID)
	}
	if params.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, params.UserID)
	}
	if params.RequestID != "" {
		conditions = append(conditions, "request_id = ?")
		args = append(args, params.RequestID)
	}
	if params.CorrelationID != "" {
		conditions = append(conditions, "correlation_id = ?")
		args = append(args, params.CorrelationID)
	}
	if params.From != "" {
		conditions = append(conditions, "ts >= ?")
		args = append(args, params.From)
	}
	if params.To != "" {
		conditions = append(conditions, "ts <= ?")
		args = append(args, params.To)
	}
	if params.Search != "" {
		conditions = append(conditions, "(message LIKE ? OR event LIKE ?)")
		args = append(args, "%"+params.Search+"%", "%"+params.Search+"%")
	}

	if len(conditions) == 0 {
		return "", nil
	}

	where := " WHERE "
	for i, cond := range conditions {
		if i > 0 {
			where += " AND "
		}
		where += cond
	}
	return where, args
}

func marshalMetadata(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
