package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"sync-win/agent/collectors"
)

var processSignalLimiter = collectors.NewProcessSignalLimiter(10, time.Minute)
var processReniceLimiter = collectors.NewProcessSignalLimiter(10, time.Minute)
var processIoniceLimiter = collectors.NewProcessSignalLimiter(10, time.Minute)

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
		guard := newProcessGuard(policy)
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
		guard := newProcessGuard(policy)
		if !processReniceLimiter.Allow(time.Now()) {
			return commandResult{"failed", "rate limited"}
		}
		if err := guard.CheckRenice(p.PID, p.Nice, p.StartTimeUnix); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, p.PID, p.Nice); err != nil {
			return commandResult{"failed", err.Error()}
		}
		return commandResult{"completed", ""}

	case "process_ionice":
		var p struct {
			PID           int   `json:"pid"`
			Class         int   `json:"class"`
			Level         int   `json:"level"`
			StartTimeUnix int64 `json:"start_time_unix"`
		}
		if err := strictDecode(payload, &p); err != nil {
			return commandResult{"failed", err.Error()}
		}
		guard := newProcessGuard(policy)
		if !processIoniceLimiter.Allow(time.Now()) {
			return commandResult{"failed", "rate limited"}
		}
		if err := guard.CheckIonice(p.PID, p.Class, p.Level, p.StartTimeUnix); err != nil {
			return commandResult{"failed", err.Error()}
		}
		if !ioniceSupported {
			return commandResult{"failed", "ionice unsupported on this architecture"}
		}
		_, _, errno := syscall.Syscall(ioprioSetNr, 1, uintptr(p.PID), uintptr((p.Class<<13)|p.Level))
		if errno != 0 {
			return commandResult{"failed", errno.Error()}
		}
		return commandResult{"completed", ""}

	default:
		return commandResult{"failed", "unsupported command type: " + cmdType}
	}
}

func newProcessGuard(policy localPolicy) collectors.ProcessGuard {
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
	return guard
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
