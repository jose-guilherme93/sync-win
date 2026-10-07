package collectors

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// maxLogMessageBytes caps a single journal line before upload.
const maxLogMessageBytes = 2000

var (
	kvSecretRE       = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|api[_-]?key)\s*[:=]\s*\S+`)
	authorizationRE  = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*.*`)
	prefixedSecretRE = regexp.MustCompile(`\b(AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]+|sk_live_[A-Za-z0-9]+|AIza[0-9A-Za-z_\-]{35})\b`)
	privateKeyRE     = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

type DeviceLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

// redactLogMessage strips common secret shapes from a journal line before it is
// uploaded, and caps its length. The system journal frequently carries tokens,
// sudo command lines and credentials echoed by services.
func redactLogMessage(message string) string {
	// Authorization values commonly span a scheme plus a token, so drop the
	// whole remainder of the line rather than a single word.
	message = authorizationRE.ReplaceAllString(message, "authorization=[REDACTED]")
	message = kvSecretRE.ReplaceAllString(message, "$1=[REDACTED]")
	message = prefixedSecretRE.ReplaceAllString(message, "[REDACTED]")
	message = privateKeyRE.ReplaceAllString(message, "[REDACTED]")
	if len(message) > maxLogMessageBytes {
		message = message[:maxLogMessageBytes]
	}
	return message
}

// CollectDeviceLogs reads recent system journal entries (last 150 lines).
//
// A failure is returned rather than swallowed. The agent runs as an
// unprivileged service user, so an unreadable journal is a real deployment
// outcome (no journald, missing read permission, a non-systemd host), and a
// silent nil is indistinguishable from a device that has nothing to report —
// which is how an empty log view became indistinguishable from a broken one.
func CollectDeviceLogs() ([]DeviceLog, error) {
	cmd := exec.Command("journalctl", "-n", "150", "--no-pager", "-o", "short-iso", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		// Stderr carries the actionable part. Without it an agent in the wrong
		// group reports only "exit status 1", which is what left a whole fleet
		// silently reporting no logs while looking healthy on the dashboard.
		return nil, fmt.Errorf("journalctl: %w: %s", err, strings.TrimSpace(stderrOf(err)))
	}
	if len(out) == 0 {
		// Success with no output is ambiguous: either the journal is genuinely
		// empty or it is unreadable and journalctl exited 0 having read
		// nothing. Say which, because the first is fine and the second is not.
		return nil, errors.New("journalctl returned no output; the journal may be empty or unreadable by the agent user (needs membership of systemd-journal or adm)")
	}

	var logs []DeviceLog
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		log := parseJournalLine(line)
		if log.Message != "" {
			log.Message = redactLogMessage(log.Message)
			logs = append(logs, log)
		}
	}
	return logs, nil
}

// stderrOf extracts the stderr text an *exec.ExitError carries. exec.Cmd.Output
// puts stderr in ExitError.Stderr only when Stderr was left nil, which is the
// case here.
func stderrOf(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(exitErr.Stderr)
	}
	return ""
}

func parseJournalLine(line string) DeviceLog {
	// Format: 2024-01-15T10:30:00+0000 hostname service[pid]: message
	log := DeviceLog{Source: "system"}

	parts := strings.SplitN(line, " ", 4)
	if len(parts) >= 4 {
		log.Timestamp = parts[0]
		// parts[1] = hostname, parts[2] = service[pid] followed by ':'.
		// Lines from sources without a pid (kernel, systemd units in some
		// formats) are written as "kernel:" with nothing in brackets, so the
		// colon has to go too. Left attached it splits one service across two
		// source buckets in the dashboard's source filter.
		service := parts[2]
		if idx := strings.Index(service, "["); idx > 0 {
			service = service[:idx]
		}
		log.Source = strings.TrimSuffix(service, ":")
		msg := parts[3]
		upper := strings.ToUpper(msg)
		if strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAIL") || strings.Contains(upper, "CRIT") {
			log.Level = "error"
		} else if strings.Contains(upper, "WARN") {
			log.Level = "warn"
		} else {
			log.Level = "info"
		}
		log.Message = msg
	} else {
		log.Message = line
		log.Level = "info"
	}
	return log
}
