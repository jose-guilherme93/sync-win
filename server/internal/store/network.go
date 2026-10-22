package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// The network view has two halves with very different tempos. The counters
// and the packet health travel with the telemetry, every ten seconds. The
// topology (gateway, resolvers, routes, neighbours) changes on the order of
// minutes, so it travels with the system inventory and is stored in its own
// column on the device row, next to the services and the ports.

// NetworkRoute is one entry of the device routing table.
type NetworkRoute struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface"`
	Metric      int    `json:"metric,omitempty"`
	Kind        string `json:"kind"`
	Family      string `json:"family"`
}

// NetworkNeighbor is one entry of the neighbour table.
type NetworkNeighbor struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac,omitempty"`
	Interface string `json:"interface"`
	State     string `json:"state,omitempty"`
	Family    string `json:"family"`
}

// WirelessLink is the radio quality of a wireless interface. The SSID and the
// channel need privileges the agent does not have, so they are not reported.
type WirelessLink struct {
	Interface string `json:"interface"`
	SignalDBM int    `json:"signal_dbm"`
	Quality   int    `json:"quality"`
	NoiseDBM  int    `json:"noise_dbm"`
}

// TunnelLink is a VPN or overlay interface on the device.
type TunnelLink struct {
	Interface string `json:"interface"`
	Kind      string `json:"kind"`
	State     string `json:"state,omitempty"`
}

// NetworkTopology is the shape of the device's network.
type NetworkTopology struct {
	DefaultGateway string            `json:"default_gateway,omitempty"`
	Gateways       []string          `json:"gateways,omitempty"`
	DNS            []string          `json:"dns,omitempty"`
	SearchDomains  []string          `json:"search_domains,omitempty"`
	Routes         []NetworkRoute    `json:"routes,omitempty"`
	Neighbors      []NetworkNeighbor `json:"neighbors,omitempty"`
	Wireless       []WirelessLink    `json:"wireless,omitempty"`
	Tunnels        []TunnelLink      `json:"tunnels,omitempty"`
	CollectedAt    string            `json:"collected_at,omitempty"`
}

// FirewallState says whether the host firewall is installed and running. The
// rules are out of reach for an unprivileged agent, so only the state of the
// unit is reported.
type FirewallState struct {
	UFW       string `json:"ufw"`
	Firewalld string `json:"firewalld"`
	Nftables  string `json:"nftables"`
}

// NetworkSection is the network half of the system inventory.
type NetworkSection struct {
	Topology NetworkTopology `json:"topology"`
	Firewall FirewallState   `json:"firewall"`
}

// UpdateNetworkSection stores the network topology of a device.
func (s *Store) UpdateNetworkSection(ctx context.Context, deviceID string, section *NetworkSection) error {
	if section == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(section)
	if err != nil {
		return err
	}
	now := timeText(time.Now().UTC())
	if _, err := s.db.ExecContext(ctx, "UPDATE devices SET network_json = ?, updated_at = ? WHERE id = ?", string(data), now, deviceID); err != nil {
		return err
	}
	return nil
}

// GetNetworkSection returns the last network topology of a device. A device
// that never reported one yields an empty section, not an error.
func (s *Store) GetNetworkSection(ctx context.Context, deviceID string) (NetworkSection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT network_json FROM devices WHERE id = ?", deviceID).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return NetworkSection{}, nil
		}
		return NetworkSection{}, err
	}
	if !raw.Valid || raw.String == "" {
		return NetworkSection{}, nil
	}
	var section NetworkSection
	if err := json.Unmarshal([]byte(raw.String), &section); err != nil {
		return NetworkSection{}, err
	}
	return section, nil
}

// NetworkHistoryPoint is one bucket of the per-interface traffic history.
// The rates are bytes per second, averaged over the samples in the bucket.
type NetworkHistoryPoint struct {
	Timestamp string  `json:"timestamp"`
	RXRate    float64 `json:"rx_rate"`
	TXRate    float64 `json:"tx_rate"`
	RXMax     float64 `json:"rx_max"`
	TXMax     float64 `json:"tx_max"`
	Samples   int     `json:"samples"`
}

// NetworkHistory is the traffic history of one device, one entry per
// interface.
type NetworkHistory struct {
	DeviceID   string                           `json:"device_id"`
	Resolution string                           `json:"resolution"`
	From       string                           `json:"from"`
	To         string                           `json:"to"`
	Interfaces map[string][]NetworkHistoryPoint `json:"interfaces"`
}

// networkHistory1m and networkHistory5m are the two resolutions kept for the
// per-interface traffic. The minute buckets are the raw signal; the five
// minute buckets are the roll-up that survives the minute data being pruned.
const (
	networkHistory1m = "1m"
	networkHistory5m = "5m"
)

// networkHistoryMaxRows bounds one history query, so a wide range cannot
// pull an unbounded number of rows into memory.
const networkHistoryMaxRows = 20000

// RecordNetworkSample folds one telemetry sample into the per-minute history
// of every interface that counts towards the fleet totals.
//
// The container plumbing is left out on purpose: the same bytes travel
// through the veth pair, the bridge and docker0, so a history that counted
// them would show three times the traffic that actually left the machine.
func (s *Store) RecordNetworkSample(deviceID string, ifaces []NetworkIface, at time.Time) error {
	if deviceID == "" || len(ifaces) == 0 {
		return nil
	}
	bucket := at.UTC().Truncate(time.Minute)
	bucketText := timeText(bucket)

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO network_history (device_id, iface, resolution, bucket, rx_sum, tx_sum, rx_max, tx_max, samples)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(device_id, iface, resolution, bucket) DO UPDATE SET
			rx_sum = rx_sum + excluded.rx_sum,
			tx_sum = tx_sum + excluded.tx_sum,
			rx_max = MAX(rx_max, excluded.rx_max),
			tx_max = MAX(tx_max, excluded.tx_max),
			samples = samples + 1`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, iface := range ifaces {
		if !iface.Counted {
			continue
		}
		if _, err := stmt.Exec(deviceID, iface.Name, networkHistory1m, bucketText,
			iface.RXRate, iface.TXRate, iface.RXRate, iface.TXRate); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetNetworkHistory returns the per-interface traffic history of a device.
//
// The resolution is chosen by the caller: the minute buckets for the last two
// days, the five minute buckets for anything older. Asking for a resolution
// that has no rows yields an empty map rather than an error, so a device that
// was offline for a week shows an empty chart and not a failure.
func (s *Store) GetNetworkHistory(deviceID, resolution string, from, to time.Time) (NetworkHistory, error) {
	if resolution == "" {
		resolution = networkHistory1m
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT iface, bucket, rx_sum, tx_sum, rx_max, tx_max, samples
		FROM network_history
		WHERE device_id = ? AND resolution = ? AND bucket >= ? AND bucket <= ?
		ORDER BY bucket ASC
		LIMIT ?`, deviceID, resolution, timeText(from), timeText(to), networkHistoryMaxRows)
	if err != nil {
		return NetworkHistory{}, err
	}
	defer rows.Close()

	history := NetworkHistory{
		DeviceID:   deviceID,
		Resolution: resolution,
		From:       timeText(from),
		To:         timeText(to),
		Interfaces: map[string][]NetworkHistoryPoint{},
	}
	for rows.Next() {
		var iface, bucket string
		var rxSum, txSum, rxMax, txMax float64
		var samples int
		if err := rows.Scan(&iface, &bucket, &rxSum, &txSum, &rxMax, &txMax, &samples); err != nil {
			return NetworkHistory{}, err
		}
		point := NetworkHistoryPoint{
			Timestamp: bucket,
			RXMax:     rxMax,
			TXMax:     txMax,
			Samples:   samples,
		}
		if samples > 0 {
			point.RXRate = rxSum / float64(samples)
			point.TXRate = txSum / float64(samples)
		}
		history.Interfaces[iface] = append(history.Interfaces[iface], point)
	}
	if err := rows.Err(); err != nil {
		return NetworkHistory{}, err
	}
	return history, nil
}

// RollupNetworkHistory folds the minute buckets that are older than two days
// into five minute buckets and then drops them.
//
// The minute data is the raw signal and it is expensive to keep: three
// interfaces on a device are 4320 rows a day. The five minute buckets keep
// the shape of the traffic for a month at a twentieth of the rows.
func (s *Store) RollupNetworkHistory() (int64, error) {
	cutoff := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT device_id, iface, bucket, rx_sum, tx_sum, rx_max, tx_max, samples
		FROM network_history
		WHERE resolution = ? AND bucket < ?`, networkHistory1m, timeText(cutoff))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type key struct {
		device string
		iface  string
		bucket string
	}
	rolled := map[key]NetworkHistoryPoint{}
	counts := map[key]int64{}
	for rows.Next() {
		var device, iface, bucket string
		var rxSum, txSum, rxMax, txMax float64
		var samples int
		if err := rows.Scan(&device, &iface, &bucket, &rxSum, &txSum, &rxMax, &txMax, &samples); err != nil {
			return 0, err
		}
		stamp, err := time.Parse(time.RFC3339, bucket)
		if err != nil {
			continue
		}
		five := stamp.Truncate(5 * time.Minute).UTC()
		k := key{device: device, iface: iface, bucket: timeText(five)}
		point := rolled[k]
		point.RXRate += rxSum
		point.TXRate += txSum
		point.Samples += samples
		if rxMax > point.RXMax {
			point.RXMax = rxMax
		}
		if txMax > point.TXMax {
			point.TXMax = txMax
		}
		rolled[k] = point
		counts[k]++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	if len(rolled) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO network_history (device_id, iface, resolution, bucket, rx_sum, tx_sum, rx_max, tx_max, samples)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, iface, resolution, bucket) DO UPDATE SET
			rx_sum = rx_sum + excluded.rx_sum,
			tx_sum = tx_sum + excluded.tx_sum,
			rx_max = MAX(rx_max, excluded.rx_max),
			tx_max = MAX(tx_max, excluded.tx_max),
			samples = samples + excluded.samples`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for k, point := range rolled {
		if _, err := stmt.Exec(k.device, k.iface, networkHistory5m, k.bucket,
			point.RXRate, point.TXRate, point.RXMax, point.TXMax, point.Samples); err != nil {
			return 0, err
		}
	}

	res, err := tx.Exec(`DELETE FROM network_history WHERE resolution = ? AND bucket < ?`, networkHistory1m, timeText(cutoff))
	if err != nil {
		return 0, err
	}
	deleted, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// CleanupNetworkHistory deletes the history rows that are older than the
// retention the server keeps for them.
func (s *Store) CleanupNetworkHistory(minuteDays, fiveMinuteDays int) (int64, error) {
	if minuteDays <= 0 {
		minuteDays = 7
	}
	if fiveMinuteDays <= 0 {
		fiveMinuteDays = 30
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var total int64
	now := time.Now().UTC()
	for _, tier := range []struct {
		resolution string
		days       int
	}{
		{networkHistory1m, minuteDays},
		{networkHistory5m, fiveMinuteDays},
	} {
		cutoff := now.AddDate(0, 0, -tier.days)
		res, err := s.db.Exec(`DELETE FROM network_history WHERE resolution = ? AND bucket < ?`, tier.resolution, timeText(cutoff))
		if err != nil {
			return total, fmt.Errorf("delete network history %s: %w", tier.resolution, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
