package collectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessSignalRequest is the decoded form of a process_signal command.
type ProcessSignalRequest struct {
	PID           int    `json:"pid"`
	Signal        string `json:"signal"`
	StartTimeUnix int64  `json:"start_time_unix"`
}

// ProcessGuard evaluates whether a signal or other action may target a process.
type ProcessGuard struct {
	ProcRoot       string
	AllowSignals   bool
	AgentPID       int
	AllowedSignals []string
	ProtectedNames []string
	ProtectedUnits []string
	MinPID         int
	MaxPID         int
}

func (g *ProcessGuard) procRoot() string {
	if g.ProcRoot == "" {
		return "/proc"
	}
	return g.ProcRoot
}

// CheckTarget is the single target-validation code path shared by signal,
// renice and ionice. It fails closed.
func (g *ProcessGuard) CheckTarget(pid int, startTimeUnix int64) error {
	minPID := g.MinPID
	if minPID <= 0 {
		minPID = 2
	}
	maxPID := g.MaxPID
	if maxPID <= 0 {
		maxPID = 4194304
	}
	if pid == 1 {
		return fmt.Errorf("pid 1 is protected")
	}
	if pid < minPID || pid > maxPID {
		return fmt.Errorf("pid out of range")
	}

	stat, err := ReadProcStat(g.procRoot(), pid)
	if err != nil {
		return fmt.Errorf("process not found")
	}
	boot := readBootTime(g.procRoot())
	liveStart := ProcessStartTimeUnix(boot, stat.StartTicks)
	// Fail closed: a missing start time cannot be verified.
	if startTimeUnix == 0 {
		return fmt.Errorf("pid reused")
	}
	if liveStart != startTimeUnix {
		return fmt.Errorf("pid reused")
	}

	if pid == g.AgentPID {
		return fmt.Errorf("protected process")
	}
	name := stat.Name
	for _, pn := range g.ProtectedNames {
		if strings.EqualFold(name, pn) {
			return fmt.Errorf("protected process")
		}
	}
	// comm file name (catches renamed/replaced binaries).
	commData, _ := os.ReadFile(filepath.Join(g.procRoot(), strconv.Itoa(pid), "comm"))
	commName := strings.TrimSpace(string(commData))
	if commName != "" {
		for _, pn := range g.ProtectedNames {
			if strings.EqualFold(commName, pn) {
				return fmt.Errorf("protected process")
			}
		}
	}
	// cgroup unit (checked before the kernel-thread heuristic so a kernel
	// thread whose unit is protected still reports "protected").
	cgData, _ := os.ReadFile(filepath.Join(g.procRoot(), strconv.Itoa(pid), "cgroup"))
	cgroup := strings.TrimSpace(string(cgData))
	line := cgroup
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	unit := parseCgroupUnitFromLine(line)
	for _, pu := range g.ProtectedUnits {
		if unit != "" && unit == pu {
			return fmt.Errorf("protected unit")
		}
	}
	// kernel thread
	cmdData, _ := os.ReadFile(filepath.Join(g.procRoot(), strconv.Itoa(pid), "cmdline"))
	if len(strings.TrimSpace(string(cmdData))) == 0 && stat.PPID == 2 {
		return fmt.Errorf("kernel thread")
	}
	if stat.State == 'Z' {
		return fmt.Errorf("zombie process")
	}
	return nil
}

// CheckProcessTarget is an alias for CheckTarget.
func (g *ProcessGuard) CheckProcessTarget(pid int, startTimeUnix int64) error {
	return g.CheckTarget(pid, startTimeUnix)
}

// Additional aliases kept for compatibility with callers/tests.
func (g *ProcessGuard) ValidateTarget(pid int, startTimeUnix int64) error {
	return g.CheckTarget(pid, startTimeUnix)
}
func (g *ProcessGuard) Check(pid int, startTimeUnix int64) error {
	return g.CheckTarget(pid, startTimeUnix)
}

func (g *ProcessGuard) CheckSignal(req ProcessSignalRequest) (syscall.Signal, error) {
	if !g.AllowSignals {
		return 0, fmt.Errorf("disabled by local policy")
	}
	// signal allowlist
	allowed := false
	for _, s := range g.AllowedSignals {
		if s == req.Signal {
			allowed = true
			break
		}
	}
	if !allowed {
		return 0, fmt.Errorf("signal not allowed")
	}
	if err := g.CheckTarget(req.PID, req.StartTimeUnix); err != nil {
		return 0, err
	}

	// map signal
	var sig syscall.Signal
	switch req.Signal {
	case "TERM":
		sig = syscall.SIGTERM
	case "KILL":
		sig = syscall.SIGKILL
	case "STOP":
		sig = syscall.SIGSTOP
	case "CONT":
		sig = syscall.SIGCONT
	default:
		return 0, fmt.Errorf("signal not allowed")
	}
	return sig, nil
}

// CheckRenice validates a renice request against the shared target check.
func (g *ProcessGuard) CheckRenice(pid int, nice int, startTimeUnix int64) error {
	if err := ValidateNicePriority(nice); err != nil {
		return err
	}
	return g.CheckTarget(pid, startTimeUnix)
}

// CheckIonice validates an ionice request against the shared target check.
func (g *ProcessGuard) CheckIonice(pid int, class, level int, startTimeUnix int64) error {
	if err := ValidateIonice(class, level); err != nil {
		return err
	}
	return g.CheckTarget(pid, startTimeUnix)
}

func parseCgroupUnitFromLine(raw string) string {
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
			return segment
		}
	}
	return ""
}

// DecodeProcessSignalPayload decodes a process_signal payload strictly.
func DecodeProcessSignalPayload(data []byte) (ProcessSignalRequest, error) {
	maxBytes := 4096
	if len(data) > maxBytes {
		return ProcessSignalRequest{}, fmt.Errorf("payload too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var req ProcessSignalRequest
	if err := dec.Decode(&req); err != nil {
		return ProcessSignalRequest{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return ProcessSignalRequest{}, fmt.Errorf("request body must contain one JSON value")
		}
		return ProcessSignalRequest{}, err
	}
	return req, nil
}

func ValidateNicePriority(n int) error {
	if n < -20 || n > 19 {
		return fmt.Errorf("nice out of range")
	}
	return nil
}

func ValidateIonice(class, level int) error {
	if class < 1 || class > 3 {
		return fmt.Errorf("ionice class out of range")
	}
	if level < 0 || level > 7 {
		return fmt.Errorf("ionice level out of range")
	}
	return nil
}

// ProcessSignalLimiter is a simple sliding-window rate limiter.
type ProcessSignalLimiter struct {
	mu         sync.Mutex
	max        int
	window     time.Duration
	timestamps []time.Time
}

func NewProcessSignalLimiter(max int, window time.Duration) *ProcessSignalLimiter {
	return &ProcessSignalLimiter{max: max, window: window}
}

func (l *ProcessSignalLimiter) Allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	filtered := l.timestamps[:0]
	for _, ts := range l.timestamps {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	l.timestamps = filtered
	if len(l.timestamps) >= l.max {
		return false
	}
	l.timestamps = append(l.timestamps, now)
	return true
}
