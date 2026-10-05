package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	syncwincrypto "sync-win/server/internal/crypto"
)

// NotificationConfig is one row of notification_configs with the config
// column already decrypted. Config is never returned to API clients as-is;
// the app layer masks non-public fields.
type NotificationConfig struct {
	OwnerID   string          `json:"owner_id"`
	Provider  string          `json:"provider"`
	Enabled   bool            `json:"enabled"`
	Events    []string        `json:"events"`
	Config    json.RawMessage `json:"-"` // decrypted secrets — never serialize
	UpdatedAt time.Time       `json:"updated_at"`
}

// NotificationEvent is one row of notification_events (the in-site inbox).
type NotificationEvent struct {
	ID        int64     `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Type      string    `json:"type"`
	DeviceID  string    `json:"device_id"`
	Hostname  string    `json:"hostname"`
	Message   string    `json:"message"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

// maxNotificationEventsPerOwner bounds the inbox; older rows are pruned on
// every insert so the table cannot grow without limit.
const maxNotificationEventsPerOwner = 200

// SaveNotificationConfig validates-and-encrypts the provider config and
// persists it. It fails if SYNCWIN_SECRET_KEY is unavailable, so credentials can
// never be written in plaintext.
func (s *Store) SaveNotificationConfig(ownerID, provider string, enabled bool, events []string, configJSON json.RawMessage) error {
	if ownerID == "" {
		return errors.New("owner_id is required")
	}
	if provider == "" {
		return errors.New("provider is required")
	}
	if len(configJSON) > 8<<10 {
		return errors.New("notification config too large")
	}
	key, err := syncwincrypto.Key()
	if err != nil {
		return err
	}
	encrypted, err := syncwincrypto.Encrypt(key, configJSON)
	if err != nil {
		return fmt.Errorf("encrypt notification config: %w", err)
	}
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeText(time.Now().UTC())
	_, err = s.db.Exec(
		`INSERT INTO notification_configs (owner_id, provider, enabled, config_enc, events, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(owner_id, provider) DO UPDATE SET
		   enabled = excluded.enabled, config_enc = excluded.config_enc,
		   events = excluded.events, updated_at = excluded.updated_at`,
		ownerID, provider, boolInt(enabled), encrypted, string(eventsJSON), now,
	)
	return err
}

// GetNotificationConfig returns one decrypted config. sql.ErrNoRows maps to
// nil, nil so callers can distinguish "not configured" from an error.
func (s *Store) GetNotificationConfig(ownerID, provider string) (*NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, err := s.scanNotificationConfig(s.db.QueryRow(
		"SELECT owner_id, provider, enabled, config_enc, events, updated_at FROM notification_configs WHERE owner_id = ? AND provider = ?",
		ownerID, provider,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cfg, err
}

// ListNotificationConfigs returns all configs of an owner, decrypted.
func (s *Store) ListNotificationConfigs(ownerID string) ([]NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT owner_id, provider, enabled, config_enc, events, updated_at FROM notification_configs WHERE owner_id = ? ORDER BY provider",
		ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	configs := make([]NotificationConfig, 0)
	for rows.Next() {
		cfg, err := s.scanNotificationConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *cfg)
	}
	return configs, rows.Err()
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanNotificationConfig(row rowScanner) (*NotificationConfig, error) {
	var cfg NotificationConfig
	var enabled int
	var enc, events, updatedAt string
	if err := row.Scan(&cfg.OwnerID, &cfg.Provider, &enabled, &enc, &events, &updatedAt); err != nil {
		return nil, err
	}
	cfg.Enabled = enabled != 0
	cfg.UpdatedAt = textTime(updatedAt)
	if err := json.Unmarshal([]byte(events), &cfg.Events); err != nil {
		cfg.Events = []string{}
	}
	if enc != "" {
		key, err := syncwincrypto.Key()
		if err != nil {
			return nil, err
		}
		plain, err := syncwincrypto.Decrypt(key, enc)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s config for %s: %w", cfg.Provider, cfg.OwnerID, err)
		}
		cfg.Config = json.RawMessage(plain)
	}
	return &cfg, nil
}

// InsertNotificationEvent appends an event to the owner inbox and prunes old
// rows past the retention cap. It returns the new row id.
func (s *Store) InsertNotificationEvent(ownerID, eventType, deviceID, hostname, message string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		"INSERT INTO notification_events (owner_id, type, device_id, hostname, message, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		ownerID, eventType, deviceID, hostname, message, now,
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := s.pruneNotificationEventsLocked(ownerID); err != nil {
		return id, err
	}
	return id, nil
}

func (s *Store) pruneNotificationEventsLocked(ownerID string) error {
	_, err := s.db.Exec(
		`DELETE FROM notification_events WHERE owner_id = ? AND id NOT IN (
			SELECT id FROM notification_events WHERE owner_id = ? ORDER BY id DESC LIMIT ?
		)`,
		ownerID, ownerID, maxNotificationEventsPerOwner,
	)
	return err
}

// ListNotificationEvents returns up to limit events with id greater than
// sinceID, oldest first. When sinceID is 0 it returns the most recent limit
// events (still ordered oldest first) so a fresh client gets context.
func (s *Store) ListNotificationEvents(ownerID string, sinceID int64, limit int) ([]NotificationEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var rows *sql.Rows
	var err error
	if sinceID > 0 {
		rows, err = s.db.Query(
			"SELECT id, owner_id, type, device_id, hostname, message, read, created_at FROM notification_events WHERE owner_id = ? AND id > ? ORDER BY id ASC LIMIT ?",
			ownerID, sinceID, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, owner_id, type, device_id, hostname, message, read, created_at FROM (
				SELECT id, owner_id, type, device_id, hostname, message, read, created_at
				FROM notification_events WHERE owner_id = ? ORDER BY id DESC LIMIT ?
			) ORDER BY id ASC`,
			ownerID, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]NotificationEvent, 0)
	for rows.Next() {
		var e NotificationEvent
		var read int
		var createdAt string
		if err := rows.Scan(&e.ID, &e.OwnerID, &e.Type, &e.DeviceID, &e.Hostname, &e.Message, &read, &createdAt); err != nil {
			return nil, err
		}
		e.Read = read != 0
		e.CreatedAt = textTime(createdAt)
		events = append(events, e)
	}
	return events, rows.Err()
}

// MarkNotificationEventsRead marks the given ids as read. Ids belonging to a
// different owner are ignored.
func (s *Store) MarkNotificationEventsRead(ownerID string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 500 {
		return errors.New("too many ids")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec("UPDATE notification_events SET read = 1 WHERE id = ? AND owner_id = ?", id, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
