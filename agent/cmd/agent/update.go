package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxAgentDownloadBytes = 128 << 20

// agentUpdatePublicKey is the base64 Ed25519 public key used to authenticate
// auto-updates. It is injected at build time with
// -ldflags "-X main.agentUpdatePublicKey=<key>". An empty key makes the updater
// fail closed: it will not install an unsigned binary.
var agentUpdatePublicKey string

// errUnitEndpointUnsupported means the server does not serve the agent unit
// template. It is distinguished from a real failure so an older server is
// tolerated while a genuine error is still reported.
var errUnitEndpointUnsupported = errors.New("server does not expose the agent unit endpoint")

type agentVersionResponse struct {
	Version string `json:"version"`
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	serverURL := fs.String("server", "http://localhost:8080", "SyncWin server URL")
	service := fs.String("service", "sync-win-agent.service", "systemd service name")
	userSystemd := fs.Bool("user-systemd", false, "use the user systemd manager")
	deviceID := fs.String("device-id", "", "device id, for the authenticated unit refresh")
	deviceToken := fs.String("device-token", "", "device token, for the authenticated unit refresh")
	if err := fs.Parse(args); err != nil {
		return err
	}
	server := strings.TrimRight(*serverURL, "/")

	// Prefer explicit credentials. This runs as root from the update service,
	// where the agent's state file resolves under a different HOME, so reading
	// state quietly yields nothing there. The flags come from the unit, which
	// already carries the token for the daemon.
	id, token := strings.TrimSpace(*deviceID), strings.TrimSpace(*deviceToken)
	if id == "" || token == "" {
		if state := loadAgentState(); state != nil {
			id, token = state.DeviceID, state.DeviceToken
		}
	}
	return updateAgentAtIdentity(executableOf(), server, *service, *userSystemd, id, token)
}

func executableOf() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		return resolved
	}
	return executable
}

func updateAgent(serverURL, service string, userSystemd bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current agent: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return updateAgentAt(executable, serverURL, service, userSystemd)
}

func updateAgentAt(executable, serverURL, service string, userSystemd bool) error {
	return updateAgentAtIdentity(executable, serverURL, service, userSystemd, "", "")
}

// updateAgentAtIdentity carries the device credentials the unit refresh needs to
// authenticate. They are optional so the existing callers and tests keep working
// without them.
func updateAgentAtIdentity(executable, serverURL, service string, userSystemd bool, deviceID, deviceToken string) error {
	if serverURL == "" {
		return fmt.Errorf("server URL is required")
	}
	remote, err := fetchAgentVersion(serverURL)
	if err != nil {
		return err
	}
	current := installedAgentVersion(executable)
	newer, err := newerAgentVersion(remote, current)
	if err != nil {
		return err
	}
	if !newer {
		fmt.Printf("SyncWin agent is current: %s\n", current)
		// A unit change is independent of the binary. An agent can be current
		// while its unit is stale or missing entirely, which is how a device
		// kept running without systemd-journal after the fix shipped.
		return reconcileUnit(serverURL, service, userSystemd, deviceID, deviceToken)
	}

	temp, err := downloadAgent(serverURL, executable)
	if err != nil {
		return err
	}
	defer os.Remove(temp)

	// Authenticate before doing anything with the binary. The signature is
	// checked first so the downloaded file is never executed (installedAgentVersion
	// runs it) until it is proven to come from the trusted key.
	signature, err := downloadAgentSignature(serverURL)
	if err != nil {
		return err
	}
	if err := verifyAgentSignature(temp, signature); err != nil {
		return err
	}
	if err := verifyAgentChecksum(serverURL, temp); err != nil {
		return err
	}
	if err := os.Chmod(temp, 0o755); err != nil {
		return fmt.Errorf("chmod downloaded agent: %w", err)
	}
	downloadedVersion := installedAgentVersion(temp)
	if downloadedVersion != remote {
		return fmt.Errorf("downloaded agent version %q does not match server version %q", downloadedVersion, remote)
	}

	backup := executable + ".rollback"
	_ = os.Remove(backup)
	if err := copyFile(executable, backup, 0o755); err != nil {
		return fmt.Errorf("backup current agent: %w", err)
	}
	if err := os.Rename(temp, executable); err != nil {
		return fmt.Errorf("install downloaded agent: %w", err)
	}
	if err := restartAgent(service, userSystemd); err != nil {
		_ = copyFile(backup, executable, 0o755)
		_ = restartAgent(service, userSystemd)
		return fmt.Errorf("new agent failed to start; rolled back: %w", err)
	}
	if err := waitForAgent(service, userSystemd); err != nil {
		_ = copyFile(backup, executable, 0o755)
		_ = restartAgent(service, userSystemd)
		return fmt.Errorf("new agent failed health check; rolled back: %w", err)
	}

	// Rewrite the unit after the binary is proven good, then restart so a unit
	// change (a new group, a flag) takes effect. Done last so a bad unit cannot
	// strand a device on an unproven binary.
	if err := reconcileUnit(serverURL, service, userSystemd, deviceID, deviceToken); err != nil {
		fmt.Printf("warning: %v\n", err)
	}

	_ = os.Remove(backup)
	fmt.Printf("SyncWin agent updated: %s -> %s\n", current, remote)
	return nil
}

// reconcileUnit rewrites the systemd unit from the server and restarts the
// service only when it actually changed.
//
// Split out so it also runs when the binary is already current: the unit is
// versioned independently of the binary, and a device that never receives a
// unit change looks perfectly healthy while a feature that depends on it is
// silently dead.
func reconcileUnit(serverURL, service string, userSystemd bool, deviceID, deviceToken string) error {
	changed, err := refreshUnitFile(serverURL, service, userSystemd, deviceID, deviceToken)
	if err != nil {
		return fmt.Errorf("could not refresh the systemd unit: %w", err)
	}
	// The remote-access helper is its own unit. A device installed before it
	// existed never got it from the installer, and a binary-only update does not
	// create a service, so reconcile it here too.
	if !userSystemd {
		if _, herr := reconcileHelperUnit(serverURL); herr != nil {
			return fmt.Errorf("could not reconcile the remote-access helper: %w", herr)
		}
	}
	if !changed {
		return nil
	}
	if err := restartAgent(service, userSystemd); err != nil {
		return fmt.Errorf("restart after unit refresh: %w", err)
	}
	fmt.Println("SyncWin agent unit refreshed")
	return nil
}

// reconcileHelperUnit installs or updates the remote-access helper unit so an
// already-installed device gains the helper without re-running the installer.
// Best-effort on servers that do not serve the template.
func reconcileHelperUnit(serverURL string) (bool, error) {
	const target = "/etc/systemd/system/sync-win-agent-helper.service"
	desired, err := fetchUnitTemplate(serverURL, "helper")
	if err != nil {
		if errors.Is(err, errUnitEndpointUnsupported) {
			return false, nil
		}
		return false, err
	}
	if current, err := os.ReadFile(target); err == nil && desired == string(current) {
		return false, nil
	}
	if err := os.WriteFile(target, []byte(desired), 0o644); err != nil {
		return false, fmt.Errorf("write helper unit: %w", err)
	}
	if err := runSystemctlArgs("daemon-reload"); err != nil {
		return false, err
	}
	if err := runSystemctlArgs("enable", "--now", "sync-win-agent-helper.service"); err != nil {
		return false, fmt.Errorf("enable remote-access helper: %w", err)
	}
	fmt.Println("SyncWin remote-access helper unit installed")
	return true, nil
}

// runSystemctlArgs runs systemctl with arbitrary arguments (the single-argument
// runSystemctl cannot express `enable --now <unit>`).
func runSystemctlArgs(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func fetchAgentVersion(serverURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(serverURL + "/api/agent/version")
	if err != nil {
		return "", fmt.Errorf("fetch agent version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version endpoint returned %d", resp.StatusCode)
	}
	var payload agentVersionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode agent version: %w", err)
	}
	if strings.TrimSpace(payload.Version) == "" {
		return "", fmt.Errorf("server returned an empty agent version")
	}
	return strings.TrimSpace(payload.Version), nil
}

func downloadAgent(serverURL, executable string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(serverURL + "/api/agent/download")
	if err != nil {
		return "", fmt.Errorf("download agent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agent download returned %d", resp.StatusCode)
	}
	file, err := os.CreateTemp(filepath.Dir(executable), ".sync-win-agent-update-*")
	if err != nil {
		return "", fmt.Errorf("create update file: %w", err)
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(resp.Body, maxAgentDownloadBytes+1))
	if err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("write agent update: %w", err)
	}
	if written > maxAgentDownloadBytes {
		file.Close()
		os.Remove(file.Name())
		return "", fmt.Errorf("agent update exceeds size limit")
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("close agent update: %w", err)
	}
	return file.Name(), nil
}

func downloadAgentSignature(serverURL string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(serverURL + "/api/agent/signature")
	if err != nil {
		return nil, fmt.Errorf("fetch agent signature: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signature endpoint returned %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return nil, fmt.Errorf("read agent signature: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(signature) == 0 {
		return nil, fmt.Errorf("invalid agent signature encoding")
	}
	return signature, nil
}

// verifyAgentSignature authenticates the downloaded binary against the public
// key embedded at build time. It fails closed when the agent was built without
// a key.
func verifyAgentSignature(path string, signature []byte) error {
	if strings.TrimSpace(agentUpdatePublicKey) == "" {
		return fmt.Errorf("agent was built without an update signing key; refusing unsigned update")
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(agentUpdatePublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid embedded update signing key")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded agent: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), data, signature) {
		return fmt.Errorf("agent update signature verification failed")
	}
	return nil
}

func verifyAgentChecksum(serverURL, path string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(serverURL + "/api/agent/checksums")
	if err != nil {
		return fmt.Errorf("fetch agent checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum endpoint returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("read agent checksum: %w", err)
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && filepath.Base(fields[1]) == "sync-win-agent" {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("server checksum for sync-win-agent is missing")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded agent: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected {
		return fmt.Errorf("agent checksum mismatch")
	}
	return nil
}

func installedAgentVersion(path string) string {
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func newerAgentVersion(remote, current string) (bool, error) {
	remoteParts, err := parseAgentVersion(remote)
	if err != nil {
		return false, fmt.Errorf("invalid server agent version %q", remote)
	}
	if current == "" || current == "unknown" {
		return true, nil
	}
	currentParts, err := parseAgentVersion(current)
	if err != nil {
		return true, nil
	}
	for i := 0; i < 3; i++ {
		if remoteParts[i] != currentParts[i] {
			return remoteParts[i] > currentParts[i], nil
		}
	}
	return false, nil
}

func parseAgentVersion(value string) ([3]int, error) {
	var result [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("invalid version")
	}
	for i, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return result, fmt.Errorf("invalid version")
		}
		result[i] = parsed
	}
	return result, nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

// refreshUnitFile rewrites the agent's systemd unit from the server.
//
// A unit change is invisible to a binary-only update: the new binary can add
// diagnostics for a journal it cannot read, but nothing adds the group that lets
// it read one. This is what makes a fix like that reach devices unattended.
//
// It fetches the raw template from the public endpoint rather than the
// credential-authenticated one. The update service on a device installed before
// that flag existed passes no credentials, and requiring them would silently
// skip exactly the devices that most need the unit repaired.
//
// The ExecStart line is preserved from the running unit, because a device behind
// a different server URL, or installed with a non-default binary path, must not
// be silently repointed by an update.
func refreshUnitFile(serverURL, service string, userSystemd bool, _ string, _ string) (bool, error) {
	if userSystemd {
		// The user-systemd fallback unit has no group requirement and is written
		// per user; leave it alone.
		return false, nil
	}

	desired, err := fetchAgentUnit(serverURL)
	if err != nil {
		// An older server without the endpoint is tolerated: the binary update is
		// the important part and must not be blocked by it. Anything else is a
		// real failure and is reported, because a silently skipped unit refresh
		// is how a device kept running without journal access after the fix.
		if errors.Is(err, errUnitEndpointUnsupported) {
			return false, nil
		}
		return false, err
	}

	target := "/etc/systemd/system/" + service
	if current, err := os.ReadFile(target); err == nil {
		desired = mergeExecStart(string(current), desired)
	}
	// The template lists groups for a general host. This one may not have docker,
	// and systemd refuses to start a unit naming a group that is absent, so the
	// line is recomputed from what exists here.
	desired = applySupplementaryGroups(desired, hostSupplementaryGroups())

	if current, err := os.ReadFile(target); err == nil && desired == string(current) {
		return false, nil
	}

	if err := os.WriteFile(target, []byte(desired), 0o644); err != nil {
		return false, fmt.Errorf("write unit %s: %w", target, err)
	}
	if err := runSystemctl("daemon-reload", userSystemd); err != nil {
		return false, fmt.Errorf("systemctl daemon-reload after unit update: %w", err)
	}
	return true, nil
}

// hostSupplementaryGroups lists the groups this host actually has.
//
// systemd will not start a unit that names a missing group, which is how an Arch
// machine without Docker ended up with a service that installed and never came
// up. docker is included only when present, and systemd-journal with adm as the
// fallback, since journal files are 0640 root:systemd-journal.
func hostSupplementaryGroups() string {
	var groups []string
	if _, err := user.LookupGroup("docker"); err == nil {
		groups = append(groups, "docker")
	}
	if _, err := user.LookupGroup("systemd-journal"); err == nil {
		groups = append(groups, "systemd-journal")
	} else if _, err := user.LookupGroup("adm"); err == nil {
		groups = append(groups, "adm")
	}
	return strings.Join(groups, " ")
}

// applySupplementaryGroups replaces the SupplementaryGroups line with the given
// groups, removes it when the host has none, and adds it under [Service] when
// the template does not carry one.
func applySupplementaryGroups(unit, groups string) string {
	lines := strings.Split(unit, "\n")
	out := make([]string, 0, len(lines)+1)
	written := false
	serviceIndex := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[Service]" {
			serviceIndex = len(out)
		}
		if strings.HasPrefix(trimmed, "SupplementaryGroups=") {
			if groups != "" && !written {
				out = append(out, "SupplementaryGroups="+groups)
				written = true
			}
			continue
		}
		out = append(out, line)
	}
	if groups != "" && !written && serviceIndex >= 0 {
		insert := serviceIndex + 1
		out = append(out, "")
		copy(out[insert+1:], out[insert:])
		out[insert] = "SupplementaryGroups=" + groups
	}
	return strings.Join(out, "\n")
}

// mergeExecStart keeps the existing ExecStart when the new template does not
// carry a usable one.
func mergeExecStart(current, desired string) string {
	if strings.Contains(desired, "ExecStart=/") {
		return desired
	}
	for _, line := range strings.Split(current, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") {
			lines := strings.Split(desired, "\n")
			for i, l := range lines {
				if strings.HasPrefix(strings.TrimSpace(l), "ExecStart=") {
					lines[i] = line
				}
			}
			return strings.Join(lines, "\n")
		}
	}
	return desired
}

func fetchAgentUnit(serverURL string) (string, error) {
	return fetchUnitTemplate(serverURL, "agent")
}

// fetchUnitTemplate fetches a raw systemd unit template by its short name from
// the public endpoint, which must not depend on credentials the caller may not
// have.
func fetchUnitTemplate(serverURL, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		serverURL+"/api/agent/units?name="+name, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// An older server has no such route, and a current one has no template on
		// disk. Either way the endpoint is simply unavailable, which must not be
		// confused with a failure worth reporting.
		return "", errUnitEndpointUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unit endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	unit := string(body)
	if !strings.Contains(unit, "[Service]") {
		return "", errors.New("unit response does not look like a systemd unit")
	}
	// Unresolved placeholders mean the server could not render it; writing this
	// would break the service.
	if strings.Contains(unit, "{{") {
		return "", errors.New("unit still contains template placeholders")
	}
	return unit, nil
}

func runSystemctl(action string, userSystemd bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", action)
	if userSystemd {
		cmd.Args = append([]string{"systemctl", "--user"}, cmd.Args[1:]...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AgentUpdateRequestPath is where the daemon asks to be updated.
//
// The daemon runs as an unprivileged service user: it can neither replace
// /usr/local/bin/sync-win-agent nor write /etc/systemd/system. A systemd path
// unit watching this file runs the root updater when it changes, so the agent
// can drive its own upgrade without holding privilege. The path must match the
// PathChanged= entry in sync-win-agent-update.path and stay inside the unit's
// ReadWritePaths.
const AgentUpdateRequestPath = "/var/lib/sync-win/update-request"

// requestUpdateForCurrentVersion writes the update request when the server is
// ahead. Safe to call on a schedule: a request is a file write, and the root
// updater is idempotent.
func requestUpdateForCurrentVersion(serverURL string) {
	remote, err := fetchAgentVersion(serverURL)
	if err != nil {
		return
	}
	current := installedAgentVersion(executableOf())
	newer, err := newerAgentVersion(remote, current)
	if err != nil || !newer {
		return
	}
	if err := writeUpdateRequest(remote); err != nil {
		log.Printf("could not request agent update: %v", err)
		return
	}
	log.Printf("agent update requested: %s -> %s (see %s)", current, remote, AgentUpdateRequestPath)
}

// writeUpdateRequest touches the request file. The content carries the version
// and a timestamp so consecutive requests differ and PathChanged fires each
// time rather than only on the first.
func writeUpdateRequest(version string) error {
	return writeUpdateRequestTo(AgentUpdateRequestPath, version)
}

func writeUpdateRequestTo(path, version string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("%s %d\n", version, time.Now().UnixNano())
	return os.WriteFile(path, []byte(body), 0o644)
}

// requestUnitRepair asks for an update run even though the binary is current.
//
// The journal permission the device Logs screen needs lives in the unit, not the
// binary, so an agent that is up to date but outside systemd-journal can only be
// fixed by the root updater. Detected from inside where the failure is visible.
func requestUnitRepair(reason string) {
	if err := writeUpdateRequest(reason); err != nil {
		log.Printf("could not request unit repair: %v", err)
		return
	}
	log.Printf("agent unit repair requested: %s (see %s)", reason, AgentUpdateRequestPath)
}

// journalAccessDenied reports whether the agent can read the system journal.
// A negative result is not fatal -- an agent outside systemd-journal still
// works, it just has no logs -- so this only drives the repair request.
func journalAccessDenied(logsStatus string) bool {
	lower := strings.ToLower(logsStatus)
	for _, marker := range []string{"permission denied", "not authorized", "access denied"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func restartAgent(service string, userSystemd bool) error {
	args := []string{"restart", service}
	if userSystemd {
		args = append([]string{"--user"}, args...)
	}
	return exec.Command("systemctl", args...).Run()
}

func waitForAgent(service string, userSystemd bool) error {
	args := []string{"is-active", "--quiet", service}
	if userSystemd {
		args = append([]string{"--user"}, args...)
	}
	for i := 0; i < 10; i++ {
		if exec.Command("systemctl", args...).Run() == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("service %s did not become active", service)
}
