package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EnrollDevice registers a device for an owner, adopting the existing record when
// the same machine enrolls again.
//
// Reinstalling the agent used to create a second device with the same hostname,
// because enrollment always inserted a fresh row and there was no way to tell
// "this is the same machine" from "this is another one". The machine identifier
// supplies that, so a reinstall adopts its own device instead of duplicating it.
//
// This does not weaken the authentication model. Authorisation still comes from
// the enrollment token, which proves the caller belongs to the owner; the machine
// identifier only chooses which of *that owner's* devices to adopt. The device
// credential is rotated on every enrollment, so a reinstall receives a new token
// rather than reviving the old one, and a hardware fingerprint remains metadata
// that never authenticates or reconnects anything.
//
// Returns the device and whether an existing record was adopted.
func (s *Store) EnrollDevice(hostname, ownerID, machineID, fingerprint string) (Device, bool, error) {
	if ownerID == "" {
		return Device{}, false, fmt.Errorf("owner_id is required")
	}
	if hostname == "" {
		hostname = "unknown"
	}
	if err := validateHostname(hostname); err != nil {
		return Device{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()
	if machineID != "" {
		var existingID string
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM devices WHERE owner_id = ? AND machine_id = ?
			 ORDER BY updated_at DESC LIMIT 1`,
			ownerID, machineID,
		).Scan(&existingID)
		switch {
		case err == nil:
			return s.adoptDeviceLocked(existingID, hostname, fingerprint)
		case !errors.Is(err, sql.ErrNoRows):
			return Device{}, false, err
		}
	}

	device, err := s.registerDeviceLocked(hostname, ownerID, fingerprint)
	if err != nil {
		return Device{}, false, err
	}
	if machineID != "" {
		if _, err := s.db.ExecContext(ctx, "UPDATE devices SET machine_id = ? WHERE id = ?", machineID, device.ID); err != nil {
			return Device{}, false, err
		}
	}
	return device, false, nil
}

// adoptDeviceLocked reuses an existing device record for a returning machine.
func (s *Store) adoptDeviceLocked(deviceID, hostname, fingerprint string) (Device, bool, error) {
	token := newToken()
	now := timeText(time.Now().UTC())

	// The fingerprint is only written when one was supplied, so a reinstall
	// without it does not erase the value already stored.
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE devices SET hostname = ?, device_token = ?,
		   hardware_fingerprint = CASE WHEN ? != '' THEN ? ELSE hardware_fingerprint END,
		   status = 'online', sync_failures = 0, last_error = '', updated_at = ?
		 WHERE id = ?`,
		hostname, hashToken(token), fingerprint, fingerprint, now, deviceID,
	)
	if err != nil {
		return Device{}, false, err
	}

	device, err := s.getDeviceLocked(deviceID)
	if err != nil {
		return Device{}, false, err
	}
	// The plaintext token exists only here; the database stores its hash.
	device.DeviceToken = token
	return device, true, nil
}
