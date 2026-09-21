package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// legacyState mirrors the pre-SQLite JSON state file so existing
// deployments upgrade in place without data loss.
type legacyState struct {
	Devices  map[string]Device         `json:"devices"`
	Files    map[string]PreferenceFile `json:"files"`
	Users    map[string]User           `json:"users"`
	Commands map[string]DeviceCommand  `json:"commands"`
	Sessions map[string]Session        `json:"sessions"`
}

const legacyStateName = "lem-store.json"

func migrateLegacyJSON(db *sql.DB, root string) error {
	legacyPath := filepath.Join(root, legacyStateName)
	data, err := os.ReadFile(legacyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy state: %w", err)
	}
	var state legacyState
	if err := json.Unmarshal(data, &state); err != nil {
		// Unreadable legacy file: keep it around for inspection and start fresh.
		_ = os.Rename(legacyPath, legacyPath+".corrupt")
		return nil
	}

	var existing int
	if err := db.QueryRow(
		"SELECT (SELECT COUNT(*) FROM devices) + (SELECT COUNT(*) FROM users) + (SELECT COUNT(*) FROM files) + (SELECT COUNT(*) FROM commands)",
	).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		// Database already populated; never overwrite live data.
		return os.Rename(legacyPath, legacyPath+".migrated")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for id, user := range state.Users {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)",
			id, user.Email, user.PasswordHash, timeText(time.Now().UTC()),
		); err != nil {
			return fmt.Errorf("migrate user %s: %w", id, err)
		}
	}
	for _, device := range state.Devices {
		if device.OwnerID == "" {
			device.OwnerID = device.UserID
		}
		if err := upsertDeviceInTx(tx, device); err != nil {
			return fmt.Errorf("migrate device %s: %w", device.ID, err)
		}
	}
	for _, file := range dedupeLegacyFiles(state.Files) {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO files (id, device_id, user_id, category, filename, relative_path, content, content_hash, size_bytes, synced_at, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			file.ID, file.DeviceID, file.UserID, file.Category, file.Filename, file.RelativePath,
			file.Content, file.ContentHash, file.SizeBytes, timeText(file.SyncedAt), file.Status,
		); err != nil {
			return fmt.Errorf("migrate file %s: %w", file.ID, err)
		}
	}
	for id, command := range state.Commands {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO commands (id, device_id, type, path, name, source, status, message, created_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, command.DeviceID, command.Type, command.Path, command.Name, command.Source,
			command.Status, command.Message, timeText(command.CreatedAt), timeText(command.CompletedAt),
		); err != nil {
			return fmt.Errorf("migrate command %s: %w", id, err)
		}
	}
	for token, session := range state.Sessions {
		if time.Since(session.CreatedAt) > sessionTTL {
			continue
		}
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO sessions (token, owner_id, created_at) VALUES (?, ?, ?)",
			token, session.OwnerID, timeText(session.CreatedAt),
		); err != nil {
			return fmt.Errorf("migrate session: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Rename(legacyPath, legacyPath+".migrated")
}

func upsertDeviceInTx(tx *sql.Tx, device Device) error {
	hardwareJSON, err := json.Marshal(device.Hardware)
	if err != nil {
		return err
	}
	appsJSON, err := json.Marshal(device.Apps)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		"INSERT OR REPLACE INTO devices ("+deviceColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		device.ID, device.UserID, device.OwnerID, device.Hostname, device.DeviceToken,
		timeText(device.LastSeenAt), timeText(device.LastSyncAt), device.SyncFailures, device.LastError, timeText(device.LastErrorAt),
		string(hardwareJSON), string(appsJSON), device.Status, timeText(device.CreatedAt), timeText(device.UpdatedAt), device.HardwareFingerprint,
	)
	return err
}

func dedupeLegacyFiles(files map[string]PreferenceFile) []PreferenceFile {
	latest := map[string]PreferenceFile{}
	for _, file := range files {
		key := preferencePathKey(file.DeviceID, file.Category, file.RelativePath, file.Filename)
		current, exists := latest[key]
		if !exists || file.SyncedAt.After(current.SyncedAt) {
			latest[key] = file
		}
	}
	result := make([]PreferenceFile, 0, len(latest))
	for _, file := range latest {
		result = append(result, file)
	}
	return result
}
