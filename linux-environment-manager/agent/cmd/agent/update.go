package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

type agentVersionResponse struct {
	Version string `json:"version"`
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	serverURL := fs.String("server", "http://localhost:8080", "LEM server URL")
	service := fs.String("service", "lem-agent.service", "systemd service name")
	userSystemd := fs.Bool("user-systemd", false, "use the user systemd manager")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return updateAgent(strings.TrimRight(*serverURL, "/"), *service, *userSystemd)
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
		fmt.Printf("LEM agent is current: %s\n", current)
		return nil
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
	_ = os.Remove(backup)
	fmt.Printf("LEM agent updated: %s -> %s\n", current, remote)
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
	file, err := os.CreateTemp(filepath.Dir(executable), ".lem-agent-update-*")
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
		if len(fields) >= 2 && filepath.Base(fields[1]) == "lem-agent" {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("server checksum for lem-agent is missing")
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
