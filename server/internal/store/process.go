package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// The History surface only stays readable with a bounded set of names per
// bucket and per legend: the busiest ones win.
const (
	maxHistoryTopPerBucket = 12
	maxHistorySeries       = 12
)

// ProcessPort is one listening socket owned by a process.
type ProcessPort struct {
	Proto string `json:"proto"`
	Port  int    `json:"port"`
	Addr  string `json:"addr"`
}

// ProcessRow mirrors the agent's process row.
type ProcessRow struct {
	PID            int           `json:"pid"`
	PPID           int           `json:"ppid"`
	Name           string        `json:"name"`
	Cmd            string        `json:"cmd"`
	User           string        `json:"user"`
	UID            int           `json:"uid"`
	State          string        `json:"state"`
	Threads        int           `json:"threads"`
	RSSBytes       uint64        `json:"rss_bytes"`
	VSZBytes       uint64        `json:"vsz_bytes"`
	SwapBytes      uint64        `json:"swap_bytes"`
	CPUPercent     float64       `json:"cpu_percent"`
	CPUTimeSeconds float64       `json:"cpu_time_seconds"`
	StartTimeUnix  int64         `json:"start_time_unix"`
	UptimeSeconds  int64         `json:"uptime_seconds"`
	IOReadBps      float64       `json:"io_read_bps"`
	IOWriteBps     float64       `json:"io_write_bps"`
	FDCount        int           `json:"fd_count"`
	OOMScore       int           `json:"oom_score"`
	Nice           int           `json:"nice"`
	Prio           int           `json:"prio"`
	Cgroup         string        `json:"cgroup"`
	Unit           string        `json:"unit"`
	Container      string        `json:"container"`
	Ports          []ProcessPort `json:"ports"`
	Kernel         bool          `json:"kernel"`
}

// ProcessStats mirrors the agent's process stats.
type ProcessStats struct {
	Total           int     `json:"total"`
	Threads         int     `json:"threads"`
	Running         int     `json:"running"`
	Sleeping        int     `json:"sleeping"`
	Stopped         int     `json:"stopped"`
	Zombie          int     `json:"zombie"`
	Blocked         int     `json:"blocked"`
	Idle            int     `json:"idle"`
	Load1           float64 `json:"load1"`
	Load5           float64 `json:"load5"`
	Load15          float64 `json:"load15"`
	PSICPUSomeAvg10 float64 `json:"psi_cpu_some_avg10"`
	PSIMemSomeAvg10 float64 `json:"psi_mem_some_avg10"`
	PSIIOSomeAvg10  float64 `json:"psi_io_some_avg10"`
	OOMKills24h     int     `json:"oom_kills_24h"`
}

// ProcessSnapshot is the stored process state for a device.
type ProcessSnapshot struct {
	Processes   []ProcessRow  `json:"processes"`
	Stats       *ProcessStats `json:"stats"`
	CollectedAt string        `json:"collected_at"`
	IOAvailable bool          `json:"io_available"`
	Collected   bool          `json:"collected"`
}

// ProcessHistory is the slow-cycle history payload.
type ProcessHistory struct {
	IntervalSeconds int                    `json:"interval_seconds"`
	Samples         []ProcessHistorySample `json:"samples"`
	Events          []ProcessHistoryEvent  `json:"events"`
}

type ProcessHistorySample struct {
	T    int64                 `json:"t"`
	Rows []ProcessHistoryPoint `json:"rows"`
}

type ProcessHistoryPoint struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	CPU        float64 `json:"cpu"`
	RSS        uint64  `json:"rss"`
	IOReadBps  float64 `json:"io_read_bps"`
	IOWriteBps float64 `json:"io_write_bps"`
}

type ProcessHistoryEvent struct {
	T        int64  `json:"t"`
	Type     string `json:"type"`
	PID      int    `json:"pid"`
	Name     string `json:"name"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

type ProcessHistoryResult struct {
	Series  []ProcessHistorySeries `json:"series"`
	Events  []ProcessHistoryEvent  `json:"events"`
	Buckets []ProcessHistoryBucket `json:"buckets"`
}

type ProcessHistorySeries struct {
	Name   string      `json:"name"`
	Points [][]float64 `json:"points"`
}

type ProcessHistoryBucket struct {
	T   int64                     `json:"t"`
	Top []ProcessHistoryBucketTop `json:"top"`
}

type ProcessHistoryBucketTop struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
}

// UpdateProcessState stores the latest process table for a device.
func (s *Store) UpdateProcessState(deviceID string, snapshot ProcessSnapshot) error {
	if snapshot.Processes == nil {
		snapshot.Processes = []ProcessRow{}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode process state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(context.Background(),
		"UPDATE devices SET process_state_json = ?, updated_at = ? WHERE id = ?",
		string(payload), timeText(time.Now().UTC()), deviceID,
	)
	if err != nil {
		return fmt.Errorf("store process state: %w", err)
	}
	return nil
}

// GetProcessState returns the last stored process state. A device that never
// reported one yields collected=false and an empty list.
func (s *Store) GetProcessState(deviceID string) (ProcessSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var raw sql.NullString
	err := s.db.QueryRowContext(context.Background(),
		"SELECT process_state_json FROM devices WHERE id = ?", deviceID).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return ProcessSnapshot{Processes: []ProcessRow{}}, nil
		}
		return ProcessSnapshot{}, err
	}
	if !raw.Valid || raw.String == "" || raw.String == "null" {
		return ProcessSnapshot{Processes: []ProcessRow{}}, nil
	}
	var snapshot ProcessSnapshot
	if err := json.Unmarshal([]byte(raw.String), &snapshot); err != nil {
		return ProcessSnapshot{}, fmt.Errorf("decode process state: %w", err)
	}
	if snapshot.Processes == nil {
		snapshot.Processes = []ProcessRow{}
	}
	return snapshot, nil
}

// QueueProcessCommand queues a process command after validating payload.
func (s *Store) QueueProcessCommand(deviceID, cmdType, payload string) (DeviceCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validTypes := map[string]bool{
		"process_signal": true, "process_renice": true, "process_ionice": true,
	}
	if !validTypes[cmdType] {
		return DeviceCommand{}, fmt.Errorf("unsupported process command type: %s", cmdType)
	}
	if len(payload) > 4096 {
		return DeviceCommand{}, fmt.Errorf("payload too large")
	}

	// strict validation
	switch cmdType {
	case "process_signal":
		var p struct {
			PID           int    `json:"pid"`
			Signal        string `json:"signal"`
			StartTimeUnix int64  `json:"start_time_unix"`
		}
		if err := strictDecodeString(payload, &p); err != nil {
			return DeviceCommand{}, err
		}
		if p.PID < 2 || p.PID > 4194304 {
			return DeviceCommand{}, fmt.Errorf("pid out of range")
		}
		if p.PID == 1 {
			return DeviceCommand{}, fmt.Errorf("pid 1 is protected")
		}
		switch p.Signal {
		case "TERM", "KILL", "STOP", "CONT":
		default:
			return DeviceCommand{}, fmt.Errorf("signal not allowed")
		}
	case "process_renice":
		var p struct {
			PID           int   `json:"pid"`
			Nice          int   `json:"nice"`
			StartTimeUnix int64 `json:"start_time_unix"`
		}
		if err := strictDecodeString(payload, &p); err != nil {
			return DeviceCommand{}, err
		}
		if p.PID < 2 || p.PID > 4194304 || p.PID == 1 {
			return DeviceCommand{}, fmt.Errorf("pid out of range")
		}
		if p.Nice < -20 || p.Nice > 19 {
			return DeviceCommand{}, fmt.Errorf("nice out of range")
		}
	case "process_ionice":
		var p struct {
			PID           int   `json:"pid"`
			Class         int   `json:"class"`
			Level         int   `json:"level"`
			StartTimeUnix int64 `json:"start_time_unix"`
		}
		if err := strictDecodeString(payload, &p); err != nil {
			return DeviceCommand{}, err
		}
		if p.PID < 2 || p.PID > 4194304 || p.PID == 1 {
			return DeviceCommand{}, fmt.Errorf("pid out of range")
		}
		if p.Class < 1 || p.Class > 3 {
			return DeviceCommand{}, fmt.Errorf("ionice class out of range")
		}
		if p.Level < 0 || p.Level > 7 {
			return DeviceCommand{}, fmt.Errorf("ionice level out of range")
		}
	}

	command := DeviceCommand{
		ID:        newID(),
		DeviceID:  deviceID,
		Type:      cmdType,
		Path:      "",
		Name:      "",
		Source:    "",
		Payload:   payload,
		Status:    "queued",
		CreatedAt: time.Now().UTC(),
	}
	return command, s.insertCommand(command)
}

func strictDecodeString(payload string, dst any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

// nonNegativeRate clamps a per-process IO rate to zero. The agent derives it
// from two counter reads, so a counter reset (or a malformed value) must not
// store a negative peak that would then break the chart's axis.
func nonNegativeRate(v float64) float64 {
	if v > 0 {
		return v
	}
	return 0
}

// migrateAddProcessHistoryIOColumns adds the per-process IO rates to a table
// that was created before they existed. CREATE TABLE IF NOT EXISTS is a no-op
// on an existing database, so the columns need adding explicitly, like the
// other migrations in this file. It is safe to run on every boot.
func (s *Store) migrateAddProcessHistoryIOColumns() error {
	ctx := context.Background()
	for _, column := range []struct{ name, definition string }{
		{"io_read_bps", "REAL NOT NULL DEFAULT 0"},
		{"io_write_bps", "REAL NOT NULL DEFAULT 0"},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('process_history') WHERE name = ?", column.name).Scan(&count); err != nil {
			return fmt.Errorf("inspect process_history.%s: %w", column.name, err)
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE process_history ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("add process_history.%s: %w", column.name, err)
			}
		}
	}
	return nil
}

// RecordProcessHistory stores process history samples and events.
func (s *Store) RecordProcessHistory(deviceID string, history ProcessHistory, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, sample := range history.Samples {
		bucket := time.Unix(sample.T, 0).UTC()
		bucketText := timeText(bucket)
		for _, row := range sample.Rows {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO process_history (device_id, bucket, resolution, name, pid, cpu_sum, cpu_max, rss_max, io_read_bps, io_write_bps, samples)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT(device_id, name, pid, resolution, bucket) DO UPDATE SET
					cpu_sum = cpu_sum + excluded.cpu_sum,
					cpu_max = MAX(cpu_max, excluded.cpu_max),
					rss_max = MAX(rss_max, excluded.rss_max),
					io_read_bps = MAX(io_read_bps, excluded.io_read_bps),
					io_write_bps = MAX(io_write_bps, excluded.io_write_bps),
					samples = samples + 1`,
				deviceID, bucketText, "1m", row.Name, row.PID, row.CPU, row.CPU, int64(row.RSS),
				nonNegativeRate(row.IOReadBps), nonNegativeRate(row.IOWriteBps))
			if err != nil {
				return err
			}
		}
	}

	for _, ev := range history.Events {
		ts := time.Unix(ev.T, 0).UTC()
		exitCode := 0
		hasExit := 0
		if ev.ExitCode != nil {
			exitCode = *ev.ExitCode
			hasExit = 1
		}
		var ec interface{}
		if hasExit == 1 {
			ec = exitCode
		} else {
			ec = nil
		}
		_, err := tx.ExecContext(ctx,
			"INSERT INTO process_events (device_id, ts, type, pid, name, exit_code) VALUES (?, ?, ?, ?, ?, ?)",
			deviceID, timeText(ts), ev.Type, ev.PID, ev.Name, ec)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetProcessHistory returns aggregated process history since from.
func (s *Store) GetProcessHistory(deviceID string, from time.Time) (ProcessHistoryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := ProcessHistoryResult{
		Series:  []ProcessHistorySeries{},
		Events:  []ProcessHistoryEvent{},
		Buckets: []ProcessHistoryBucket{},
	}

	// Build series + buckets from process_history.
	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	rows, queryErr := s.db.QueryContext(qctx, `
		SELECT bucket, name, pid, cpu_sum, cpu_max, rss_max, io_read_bps, io_write_bps, samples
		FROM process_history
		WHERE device_id = ? AND bucket >= ?
		ORDER BY bucket ASC`,
		deviceID, timeText(from))
	if queryErr != nil {
		return result, queryErr
	}
	defer func() { _ = rows.Close() }()

	// Rows are per (bucket, name, pid). A name with several pids (nginx
	// workers, systemd helpers) and every sample inside the same bucket must
	// fold into one point per name, otherwise the heatmap repeats a row and
	// the legend repeats a series.
	type aggKey struct {
		bucket int64
		name   string
	}
	type aggValue struct {
		cpuSum, cpuMax, rssMax, ioReadMax, ioWriteMax float64
		samples                                       int
	}
	aggs := make(map[aggKey]*aggValue)
	var bucketOrder []int64
	seenBucket := make(map[int64]struct{})

	for rows.Next() {
		var bucketText, name string
		var pid int
		var cpuSum, cpuMax, ioReadMax, ioWriteMax float64
		var rssMax int64
		var samples int
		if err := rows.Scan(&bucketText, &name, &pid, &cpuSum, &cpuMax, &rssMax, &ioReadMax, &ioWriteMax, &samples); err != nil {
			return result, err
		}
		bucketTime, _ := time.Parse("2006-01-02T15:04:05Z", bucketText)
		if bucketTime.IsZero() {
			if t2, err := time.Parse(time.RFC3339, bucketText); err == nil {
				bucketTime = t2
			}
		}
		bucketUnix := bucketTime.Unix()
		if bucketTime.IsZero() {
			bucketUnix = 0
		}
		key := aggKey{bucket: bucketUnix, name: name}
		a, ok := aggs[key]
		if !ok {
			a = &aggValue{}
			aggs[key] = a
		}
		a.cpuSum += cpuSum
		if cpuMax > a.cpuMax {
			a.cpuMax = cpuMax
		}
		if float64(rssMax) > a.rssMax {
			a.rssMax = float64(rssMax)
		}
		if ioReadMax > a.ioReadMax {
			a.ioReadMax = ioReadMax
		}
		if ioWriteMax > a.ioWriteMax {
			a.ioWriteMax = ioWriteMax
		}
		a.samples += samples
		if _, ok := seenBucket[bucketUnix]; !ok {
			seenBucket[bucketUnix] = struct{}{}
			bucketOrder = append(bucketOrder, bucketUnix)
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	avgOf := func(a *aggValue) float64 {
		if a.samples > 0 {
			return a.cpuSum / float64(a.samples)
		}
		return a.cpuMax
	}

	seriesMap := make(map[string]*ProcessHistorySeries)
	peak := make(map[string]float64)
	for _, bucketUnix := range bucketOrder {
		bk := &ProcessHistoryBucket{T: bucketUnix, Top: []ProcessHistoryBucketTop{}}
		for key, a := range aggs {
			if key.bucket != bucketUnix {
				continue
			}
			cpuAvg := avgOf(a)
			if cpuAvg > peak[key.name] {
				peak[key.name] = cpuAvg
			}
			series, ok := seriesMap[key.name]
			if !ok {
				series = &ProcessHistorySeries{Name: key.name, Points: [][]float64{}}
				seriesMap[key.name] = series
			}
			// [unix, cpuAvg, rssMax, ioReadMax, ioWriteMax]. The first three
			// keep their old positions so an older dashboard still reads them.
			series.Points = append(series.Points, []float64{float64(bucketUnix), cpuAvg, a.rssMax, a.ioReadMax, a.ioWriteMax})
			bk.Top = append(bk.Top, ProcessHistoryBucketTop{Name: key.name, CPU: cpuAvg})
		}
		sortBucketTop(bk.Top)
		if len(bk.Top) > maxHistoryTopPerBucket {
			bk.Top = bk.Top[:maxHistoryTopPerBucket]
		}
		result.Buckets = append(result.Buckets, *bk)
	}

	names := make([]string, 0, len(seriesMap))
	for name := range seriesMap {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if peak[names[i]] != peak[names[j]] {
			return peak[names[i]] > peak[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > maxHistorySeries {
		names = names[:maxHistorySeries]
	}
	for _, name := range names {
		result.Series = append(result.Series, *seriesMap[name])
	}

	// Events
	evRows, eventErr := s.db.QueryContext(qctx, `
		SELECT ts, type, pid, name, exit_code
		FROM process_events
		WHERE device_id = ? AND ts >= ?
		ORDER BY ts ASC`, deviceID, timeText(from))
	if eventErr != nil {
		return result, eventErr
	}
	defer func() { _ = evRows.Close() }()
	for evRows.Next() {
		var tsText, typ, name string
		var pid int
		var exitCode sql.NullInt64
		if err := evRows.Scan(&tsText, &typ, &pid, &name, &exitCode); err != nil {
			return result, err
		}
		ev := ProcessHistoryEvent{T: 0, Type: typ, PID: pid, Name: name}
		if t2, err := time.Parse(time.RFC3339, tsText); err == nil {
			ev.T = t2.Unix()
		}
		if exitCode.Valid {
			v := int(exitCode.Int64)
			ev.ExitCode = &v
		}
		result.Events = append(result.Events, ev)
	}
	if err := evRows.Err(); err != nil {
		return result, err
	}

	if result.Series == nil {
		result.Series = []ProcessHistorySeries{}
	}
	if result.Buckets == nil {
		result.Buckets = []ProcessHistoryBucket{}
	}
	if result.Events == nil {
		result.Events = []ProcessHistoryEvent{}
	}
	return result, nil
}

func sortBucketTop(top []ProcessHistoryBucketTop) {
	// simple insertion sort by cpu desc
	for i := 1; i < len(top); i++ {
		j := i
		for j > 0 && top[j-1].CPU < top[j].CPU {
			top[j-1], top[j] = top[j], top[j-1]
			j--
		}
	}
}

// Need bytes import
var _ = bytes.NewReader
