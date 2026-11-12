package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The automatic-update capability is systemd units, not a policy key, so
// enabling it later cannot be a policy write. `sync-win-agent set update on`
// installs the same three units the installer does, from the server's own
// templates, which is also the repair path for a device whose installer
// skipped them.
const (
	updateUnitDir     = "/etc/systemd/system"
	updateServiceUnit = "sync-win-agent-update.service"
	updateTimerUnit   = "sync-win-agent-update.timer"
	updatePathUnit    = "sync-win-agent-update.path"
)

// cmdSetUpdate installs and enables (or disables and removes) the automatic
// update units. It runs as root, the same way the installer does.
func cmdSetUpdate(enable bool) error {
	if !enable {
		for _, unit := range []string{updateTimerUnit, updatePathUnit} {
			_ = runSystemctlArgs("disable", "--now", unit)
		}
		for _, unit := range []string{updateServiceUnit, updateTimerUnit, updatePathUnit} {
			if err := os.Remove(filepath.Join(updateUnitDir, unit)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", unit, err)
			}
		}
		_ = runSystemctlArgs("daemon-reload")
		fmt.Println("ok: automatic updates disabled")
		return nil
	}

	service := "sync-win-agent.service"
	unitPath := filepath.Join(updateUnitDir, service)
	if _, err := os.Stat(unitPath); err != nil {
		// The user-systemd install keeps its unit elsewhere; fall back to the
		// default name but still look for the URL.
		unitPath = ""
	}

	serverURL := discoverServerURL(service, false)
	deviceID, deviceToken := "", ""
	deviceTokenFile := ""
	if unitPath != "" {
		if data, err := os.ReadFile(unitPath); err == nil {
			deviceID = unitFlagValue(string(data), "--device-id")
			deviceToken = unitFlagValue(string(data), "--device-token")
			deviceTokenFile = unitFlagValue(string(data), "--device-token-file")
			if serverURL == "" {
				serverURL = serverURLFromUnit(string(data))
			}
		}
	}
	if deviceToken == "" && deviceTokenFile != "" {
		if data, err := os.ReadFile(deviceTokenFile); err == nil {
			deviceToken = strings.TrimSpace(string(data))
		}
	}
	if deviceToken == "" {
		// Last resort: the installer's default token file location.
		if data, err := os.ReadFile("/etc/sync-win/device-token"); err == nil {
			deviceToken = strings.TrimSpace(string(data))
		}
	}
	if serverURL == "" || deviceID == "" || deviceToken == "" {
		return errors.New("cannot read the agent unit's server, device id and token; re-run the installer instead")
	}

	values := map[string]string{
		"{{AGENT_BINARY}}": executableOf(),
		"{{SERVER_URL}}":   strings.TrimRight(serverURL, "/"),
		"{{SERVICE_NAME}}": service,
		"{{DEVICE_ID}}":    deviceID,
		"{{DEVICE_TOKEN}}": deviceToken,
		"{{USER_FLAG}}":    "",
	}
	units := map[string]string{
		"update-service": updateServiceUnit,
		"update-timer":   updateTimerUnit,
		"update-path":    updatePathUnit,
	}
	for template, file := range units {
		raw, err := fetchUnitBody(serverURL, template)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", file, err)
		}
		rendered := raw
		for placeholder, value := range values {
			rendered = strings.ReplaceAll(rendered, placeholder, value)
		}
		if strings.Contains(rendered, "{{") {
			return fmt.Errorf("%s still contains placeholders", file)
		}
		if err := os.WriteFile(filepath.Join(updateUnitDir, file), []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file, err)
		}
	}

	// The daemon asks for an update by touching this file; the path unit needs
	// it to exist and to be writable by the agent user.
	if err := ensureUpdateRequestFile(); err != nil {
		return err
	}
	if err := runSystemctlArgs("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	for _, unit := range []string{updateTimerUnit, updatePathUnit} {
		if err := runSystemctlArgs("enable", "--now", unit); err != nil {
			return fmt.Errorf("enable %s: %w", unit, err)
		}
	}
	fmt.Println("ok: automatic updates enabled (timer every 15 minutes, plus on demand)")
	return nil
}

// ensureUpdateRequestFile creates the state file the daemon writes to request
// an update, owned by the agent user when that user exists.
func ensureUpdateRequestFile() error {
	const path = "/var/lib/sync-win/update-request"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	return file.Close()
}

// unitFlagValue returns the value of a flag in the unit's ExecStart, in either
// `--flag value` or `--flag=value` form.
func unitFlagValue(unit, flag string) string {
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "ExecStart="))
		for i, field := range fields {
			if field == flag {
				if i+1 < len(fields) {
					return strings.Trim(fields[i+1], "\"'")
				}
				return ""
			}
			if strings.HasPrefix(field, flag+"=") {
				return strings.Trim(strings.TrimPrefix(field, flag+"="), "\"'")
			}
		}
	}
	return ""
}
