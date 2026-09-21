package collectors

import (
	"os/exec"
	"strings"
)

type DeviceLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

// CollectDeviceLogs reads recent system journal entries (last 150 lines).
func CollectDeviceLogs() []DeviceLog {
	cmd := exec.Command("journalctl", "-n", "150", "--no-pager", "-o", "short-iso", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var logs []DeviceLog
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		log := parseJournalLine(line)
		if log.Message != "" {
			logs = append(logs, log)
		}
	}
	return logs
}

func parseJournalLine(line string) DeviceLog {
	// Format: 2024-01-15T10:30:00+0000 hostname service[pid]: message
	log := DeviceLog{Source: "system"}

	parts := strings.SplitN(line, " ", 4)
	if len(parts) >= 4 {
		log.Timestamp = parts[0]
		// parts[1] = hostname, parts[2] = service[pid]
		service := parts[2]
		if idx := strings.Index(service, "["); idx > 0 {
			service = service[:idx]
		}
		log.Source = service
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
