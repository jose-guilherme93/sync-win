package store

import (
	"encoding/json"
	"math"
	"sync"
	"time"
)

// Aggregation thresholds: how many raw points to buffer before aggregating.
const (
	rawPer1m = 6   // 6 × 10s = 60s → 1-minute aggregate
	rawPer5m = 30  // 30 × 1m = 30m → 5-minute aggregate (but we use 1m points)
	rawPer1h = 12  // 12 × 5m = 60m → 1-hour aggregate
)

// TelemetryDownsampled is a single aggregated telemetry record.
type TelemetryDownsampled struct {
	ID           int64   `json:"id,omitempty"`
	DeviceID     string  `json:"device_id"`
	Timestamp    string  `json:"timestamp"`
	Resolution   string  `json:"resolution"`
	CPUAvg       float64 `json:"cpu_avg"`
	CPUMin       float64 `json:"cpu_min"`
	CPUMax       float64 `json:"cpu_max"`
	MemAvg       float64 `json:"mem_avg"`
	MemMin       float64 `json:"mem_min"`
	MemMax       float64 `json:"mem_max"`
	NetRXAvg     float64 `json:"net_rx_avg"`
	NetTXAvg     float64 `json:"net_tx_avg"`
	TempAvg      float64 `json:"temp_avg"`
	TempMax      float64 `json:"temp_max"`
	PowerAvg     float64 `json:"power_avg"`
	SampleCount  int     `json:"sample_count"`
}

// rawSample holds a parsed raw telemetry point for aggregation.
type rawSample struct {
	Timestamp time.Time
	CPU       float64
	MemPct    float64
	NetRX     float64
	NetTX     float64
	Temp      float64
	Power     float64
}

// DeviceAggState tracks aggregation counters per device.
type DeviceAggState struct {
	mu       sync.Mutex
	rawBuf   []rawSample // buffer for raw → 1m aggregation
	count1m  int         // count of 1m points buffered → 5m
	buf5m    []TelemetryDownsampled
	count5h  int         // count of 5m points buffered → 1h
}

// Aggregator manages per-device aggregation state.
type Aggregator struct {
	mu      sync.Mutex
	devices map[string]*DeviceAggState
	store   *Store
}

// NewAggregator creates an Aggregator attached to a Store.
func NewAggregator(s *Store) *Aggregator {
	return &Aggregator{
		devices: make(map[string]*DeviceAggState),
		store:   s,
	}
}

// FeedRaw adds a raw telemetry point and triggers aggregation when thresholds are met.
func (a *Aggregator) FeedRaw(deviceID string, payload []byte, ts time.Time) {
	sample := parseRawPayload(payload, ts)
	if sample == nil {
		return
	}

	a.mu.Lock()
	state, ok := a.devices[deviceID]
	if !ok {
		state = &DeviceAggState{}
		a.devices[deviceID] = state
	}
	a.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	state.rawBuf = append(state.rawBuf, *sample)

	// Aggregate raw → 1m when buffer is full
	if len(state.rawBuf) >= rawPer1m {
		agg := aggregateRaw(state.rawBuf)
		agg.DeviceID = deviceID
		agg.Resolution = "1m"
		_ = a.store.AppendTelemetryDownsampled(agg)
		state.rawBuf = state.rawBuf[:0]

		state.count1m++
		state.buf5m = append(state.buf5m, agg)

		// Aggregate 1m → 5m when buffer is full
		if len(state.buf5m) >= rawPer5m {
			agg5m := aggregateDownsampled(state.buf5m)
			agg5m.DeviceID = deviceID
			agg5m.Resolution = "5m"
			_ = a.store.AppendTelemetryDownsampled(agg5m)
			state.buf5m = state.buf5m[:0]

			state.count5h++
			// Aggregate 5m → 1h every rawPer1h cycles
			if state.count5h >= rawPer1h {
				// Fetch last 12 5m points from DB for accurate 1h aggregation
				points, err := a.store.GetDownsampledRange(deviceID, "5m", time.Now().UTC().Add(-2*time.Hour), time.Now().UTC())
				if err == nil && len(points) >= rawPer1h {
					agg1h := aggregateDownsampled(points)
					agg1h.DeviceID = deviceID
					agg1h.Resolution = "1h"
					_ = a.store.AppendTelemetryDownsampled(agg1h)
				}
				state.count5h = 0
			}
		}
	}
}

// parseRawPayload extracts numeric values from a raw HardwareStats JSON payload.
func parseRawPayload(payload []byte, ts time.Time) *rawSample {
	var hw HardwareStats
	if err := json.Unmarshal(payload, &hw); err != nil {
		return nil
	}
	var memPct float64
	if hw.MemoryTotalBytes > 0 {
		memPct = float64(hw.MemoryUsedBytes) / float64(hw.MemoryTotalBytes) * 100
	}
	var netRX, netTX float64
	if hw.NetworkIFaces != nil {
		for _, iface := range hw.NetworkIFaces {
			if iface.Name == "lo" || iface.Name == "docker0" {
				continue
			}
			netRX += iface.RXRate
			netTX += iface.TXRate
		}
	}
	return &rawSample{
		Timestamp: ts,
		CPU:       hw.CPUUsagePercent,
		MemPct:    memPct,
		NetRX:     netRX,
		NetTX:     netTX,
		Temp:      hw.CPUTemperature,
		Power:     hw.PowerWatts,
	}
}

// aggregateRaw computes avg/min/max from a slice of raw samples.
func aggregateRaw(samples []rawSample) TelemetryDownsampled {
	n := float64(len(samples))
	if n == 0 {
		return TelemetryDownsampled{SampleCount: 0}
	}

	var cpuSum, cpuMin, cpuMax float64
	var memSum, memMin, memMax float64
	var netRXSum, netTXSum float64
	var tempSum, tempMax, powerSum float64

	cpuMin = math.MaxFloat64
	memMin = math.MaxFloat64
	tempMax = -math.MaxFloat64

	for _, s := range samples {
		cpuSum += s.CPU
		if s.CPU < cpuMin { cpuMin = s.CPU }
		if s.CPU > cpuMax { cpuMax = s.CPU }

		memSum += s.MemPct
		if s.MemPct < memMin { memMin = s.MemPct }
		if s.MemPct > memMax { memMax = s.MemPct }

		netRXSum += s.NetRX
		netTXSum += s.NetTX

		tempSum += s.Temp
		if s.Temp > tempMax { tempMax = s.Temp }

		powerSum += s.Power
	}

	lastTs := samples[len(samples)-1].Timestamp
	return TelemetryDownsampled{
		Timestamp:   lastTs.UTC().Format(time.RFC3339),
		CPUAvg:      cpuSum / n,
		CPUMin:      cpuMin,
		CPUMax:      cpuMax,
		MemAvg:      memSum / n,
		MemMin:      memMin,
		MemMax:      memMax,
		NetRXAvg:    netRXSum / n,
		NetTXAvg:    netTXSum / n,
		TempAvg:     tempSum / n,
		TempMax:     tempMax,
		PowerAvg:    powerSum / n,
		SampleCount: len(samples),
	}
}

// aggregateDownsampled computes avg/min/max from a slice of downsampled records.
func aggregateDownsampled(records []TelemetryDownsampled) TelemetryDownsampled {
	n := float64(len(records))
	if n == 0 {
		return TelemetryDownsampled{SampleCount: 0}
	}

	var cpuAvgSum, cpuMin, cpuMax float64
	var memAvgSum, memMin, memMax float64
	var netRXSum, netTXSum float64
	var tempAvgSum, tempMax, powerSum float64
	var totalSamples int

	cpuMin = math.MaxFloat64
	memMin = math.MaxFloat64
	tempMax = -math.MaxFloat64

	for _, r := range records {
		cpuAvgSum += r.CPUAvg
		if r.CPUMin < cpuMin { cpuMin = r.CPUMin }
		if r.CPUMax > cpuMax { cpuMax = r.CPUMax }

		memAvgSum += r.MemAvg
		if r.MemMin < memMin { memMin = r.MemMin }
		if r.MemMax > memMax { memMax = r.MemMax }

		netRXSum += r.NetRXAvg
		netTXSum += r.NetTXAvg

		tempAvgSum += r.TempAvg
		if r.TempMax > tempMax { tempMax = r.TempMax }

		powerSum += r.PowerAvg
		totalSamples += r.SampleCount
	}

	lastTs := records[len(records)-1].Timestamp
	return TelemetryDownsampled{
		Timestamp:   lastTs,
		CPUAvg:      cpuAvgSum / n,
		CPUMin:      cpuMin,
		CPUMax:      cpuMax,
		MemAvg:      memAvgSum / n,
		MemMin:      memMin,
		MemMax:      memMax,
		NetRXAvg:    netRXSum / n,
		NetTXAvg:    netTXSum / n,
		TempAvg:     tempAvgSum / n,
		TempMax:     tempMax,
		PowerAvg:    powerSum / n,
		SampleCount: totalSamples,
	}
}

// ResolutionForInterval returns the best resolution for a given time range.
func ResolutionForInterval(from, to time.Time) string {
	dur := to.Sub(from)
	switch {
	case dur <= 2*time.Hour:
		return "raw"
	case dur <= 8*time.Hour:
		return "1m"
	case dur <= 3*24*time.Hour:
		return "5m"
	default:
		return "1h"
	}
}
