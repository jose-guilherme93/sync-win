package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sync-win/agent/collectors"
)

var processSignalLimiter = collectors.NewProcessSignalLimiter(10, time.Minute)

func executeProcessCommand(cmdType string, payload string, policy localPolicy) commandResult {
	if !policy.AllowProcessSignals {
		return commandResult{"failed", "command disabled by local policy"}
	}

	maxBytes := syncwinContract.ProcessPayloadMaxBytes()
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if len(payload) > maxBytes {
		return commandResult{"failed", "payload too large"}
	}

	switch cmdType {
	case "process_signal":
		req, err := collectors.DecodeProcessSignalPayload([]byte(payload))
		if err != nil {
			return commandResult{"failed", err.Error()}
		}
		guard := collectors.ProcessGuard{
			ProcRoot:       "/proc",
			AllowSignals:   policy.AllowProcessSignals,
			AgentPID:       os.Getpid(),
			AllowedSignals: syncwinContract.Processes.AllowedSignals,
			ProtectedNames: syncwinContract.Processes.ProtectedNames,
			ProtectedUnits: syncwinContract.Processes.ProtectedUnits,
			MinPID:         syncwinContract.Processes.MinPID,
			MaxPID:         syncwinContract.Processes.MaxPID,
		}
		if guard.MinPID <= 0 {
			guard.MinPID = 2
		}
		if guard.MaxPID <= 0 {
			guard.MaxPID = 4194304
		}
		if !processSignalLimiter.Allow(time.Now()) {
			return commandResult{"failed", "rate limited"}
		}
		sig, err := guard.CheckSignal(req)
		if err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := syscall.Kill(req.PID, sig); err != nil {
			return commandResult{"failed", err.Error()}
		}
		return commandResult{"completed", ""}

	case "process_renice":
		var p struct {
			PID           int   `json:"pid"`
			Nice          int   `json:"nice"`
			StartTimeUnix int64 `json:"start_time_unix"`
		}
		if err := strictDecode(payload, &p); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := collectors.ValidateNicePriority(p.Nice); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := checkProcessTarget(p.PID, p.StartTimeUnix); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, p.PID, p.Nice); err != nil {
			return commandResult{"failed", err.Error()}
		}
		return commandResult{"completed", ""}

	case "process_ionice":
		var p struct {
			PID           int `json:"pid"`
			Class         int `json:"class"`
			Level         int `json:"level"`
			StartTimeUnix int64 `json:"start_time_unix"`
		}
		if err := strictDecode(payload, &p); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := collectors.ValidateIonice(p.Class, p.Level); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := checkProcessTarget(p.PID, p.StartTimeUnix); err != nil {
			return commandResult{"failed", err.Error()}
		}
		// Best-effort: apply via ioprio_set when possible.
		_, _, errno := syscall.Syscall(251, 1, uintptr(p.PID), uintptr((p.Class<<13)|p.Level))
		if errno != 0 {
			return commandResult{"failed", errno.Error()}
		}
		return commandResult{"completed", ""}

	default:
		return commandResult{"failed", "unsupported command type: " + cmdType}
	}
}

func strictDecode(payload string, dst any) error {
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

func checkProcessTarget(pid int, startTimeUnix int64) error {
	minPID := 2
	maxPID := 4194304
	if syncwinContract.Processes.MinPID > 0 {
		minPID = syncwinContract.Processes.MinPID
	}
	if syncwinContract.Processes.MaxPID > 0 {
		maxPID = syncwinContract.Processes.MaxPID
	}
	if pid == 1 {
		return fmt.Errorf("pid 1 is protected")
	}
	if pid < minPID || pid > maxPID {
		return fmt.Errorf("pid out of range")
	}
	stat, err := collectors.ReadProcStat("/proc", pid)
	if err != nil {
		return fmt.Errorf("process not found")
	}
	boot := readBootTimeProc("/proc")
	liveStart := collectors.ProcessStartTimeUnix(boot, stat.StartTicks)
	if startTimeUnix != 0 && liveStart != startTimeUnix {
		return fmt.Errorf("pid reused")
	}
	// protected names
	name := stat.Name
	protectedNames := syncwinContract.Processes.ProtectedNames
	for _, pn := range protectedNames {
		if strings.EqualFold(name, pn) {
			return fmt.Errorf("protected process")
		}
	}
	if os.Getpid() == pid {
		return fmt.Errorf("protected process")
	}
	// kernel thread heuristic: no cmdline
	cmdData, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if len(strings.TrimSpace(string(cmdData))) == 0 && stat.PPID == 2 {
		return fmt.Errorf("kernel thread")
	}
	if stat.State == 'Z' {
		return fmt.Errorf("zombie process")
	}
	// protected units: need cgroup parse; quick check via collectors? Use unit from cgroup.
	cgData, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	cgroup := strings.TrimSpace(string(cgData))
	// parse first line
	line := cgroup
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	unit, _ := parseCgroupUnit(line)
	for _, pu := range syncwinContract.Processes.ProtectedUnits {
		if unit == pu {
			return fmt.Errorf("protected unit")
		}
	}
	_ = filepath.Join
	return nil
}

func readBootTimeProc(procRoot string) int64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

func parseCgroupUnit(raw string) (string, string) {
	// reuse collectors? not exported. Minimal: derive unit only.
	line := raw
	if strings.Count(line, ":") >= 2 {
		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			line = line[idx+1:]
		}
	}
	for _, segment := range strings.Split(line, "/") {
		if segment == "" {
			continue
		}
		if strings.HasSuffix(segment, ".service") || strings.HasSuffix(segment, ".scope") {
			return segment, ""
		}
	}
	return "", ""
}
