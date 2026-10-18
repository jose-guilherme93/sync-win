package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// maxAlertIDLength bounds the identifier a client may store. The id is built by
// the dashboard as "<device id>:<rule key>", so it is short; the cap only stops
// a client from using this table as arbitrary storage.
const maxAlertIDLength = 300

var (
	ErrAlertIDRequired = errors.New("alert id is required")
	ErrAlertIDTooLong  = errors.New("alert id too long")
	ErrAlertIDInvalid  = errors.New("alert id must be valid UTF-8 without control characters")
)

// AlertAck is one acknowledged finding. The alert itself is not stored: the
// dashboard derives alerts from the current telemetry, and the ack is what has
// to survive a reload and follow the account to another browser.
type AlertAck struct {
	ID      string `json:"id"`
	AckedAt string `json:"acked_at"`
}

func validateAlertID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrAlertIDRequired
	}
	if len(id) > maxAlertIDLength {
		return "", ErrAlertIDTooLong
	}
	if !utf8.ValidString(id) {
		return "", ErrAlertIDInvalid
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return "", ErrAlertIDInvalid
		}
	}
	return id, nil
}

// ListAlertAcks returns the acknowledgements stored for an owner, newest first.
func (s *Store) ListAlertAcks(ownerID string) ([]AlertAck, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT alert_id, acked_at FROM alert_acks WHERE owner_id = ? ORDER BY acked_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	acks := make([]AlertAck, 0)
	for rows.Next() {
		var ack AlertAck
		if err := rows.Scan(&ack.ID, &ack.AckedAt); err != nil {
			return nil, err
		}
		acks = append(acks, ack)
	}
	return acks, rows.Err()
}

// AckAlert stores an acknowledgement. Re-acking the same id keeps the original
// timestamp, so a double click does not look like a second event.
func (s *Store) AckAlert(ownerID, alertID string) (AlertAck, error) {
	id, err := validateAlertID(alertID)
	if err != nil {
		return AlertAck{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := timeText(time.Now().UTC())
	_, err = s.db.ExecContext(
		context.Background(),
		`INSERT INTO alert_acks (owner_id, alert_id, acked_at) VALUES (?, ?, ?)
		 ON CONFLICT(owner_id, alert_id) DO NOTHING`,
		ownerID, id, now,
	)
	if err != nil {
		return AlertAck{}, err
	}

	var ackedAt string
	if err := s.db.QueryRowContext(
		context.Background(),
		`SELECT acked_at FROM alert_acks WHERE owner_id = ? AND alert_id = ?`,
		ownerID, id,
	).Scan(&ackedAt); err != nil {
		return AlertAck{}, err
	}
	return AlertAck{ID: id, AckedAt: ackedAt}, nil
}

// UnackAlert removes one acknowledgement. Removing an absent ack is not an
// error: the client's view may be stale and the outcome is the same.
func (s *Store) UnackAlert(ownerID, alertID string) error {
	id, err := validateAlertID(alertID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.ExecContext(
		context.Background(),
		`DELETE FROM alert_acks WHERE owner_id = ? AND alert_id = ?`,
		ownerID, id,
	)
	return err
}

// ClearAlertAcks removes every acknowledgement for an owner and returns how
// many rows were deleted.
func (s *Store) ClearAlertAcks(ownerID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.ExecContext(context.Background(), `DELETE FROM alert_acks WHERE owner_id = ?`, ownerID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
