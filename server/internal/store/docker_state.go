package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// UpdateDockerState stores the latest container list for a device. The list is
// sampled slowly by the agent and lives in its own column so it is not copied
// into every telemetry_raw row.
func (s *Store) UpdateDockerState(deviceID string, containers []DockerContainer) error {
	if containers == nil {
		containers = []DockerContainer{}
	}
	payload, err := json.Marshal(containers)
	if err != nil {
		return fmt.Errorf("encode docker state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(context.Background(),
		"UPDATE devices SET docker_state_json = ?, updated_at = ? WHERE id = ?",
		string(payload), timeText(time.Now().UTC()), deviceID,
	)
	if err != nil {
		return fmt.Errorf("store docker state: %w", err)
	}
	return nil
}

// GetDockerStateContainers returns the last stored container list. A device that
// has never been sampled returns an empty slice, not an error.
func (s *Store) GetDockerStateContainers(deviceID string) ([]DockerContainer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var raw string
	err := s.db.QueryRowContext(context.Background(),
		"SELECT COALESCE(docker_state_json, '[]') FROM devices WHERE id = ?", deviceID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	containers := make([]DockerContainer, 0)
	if raw == "" {
		return containers, nil
	}
	if err := json.Unmarshal([]byte(raw), &containers); err != nil {
		return nil, fmt.Errorf("decode docker state: %w", err)
	}
	return containers, nil
}
