package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"lem/agent/collectors"
	"lem/agent/internal/contract"
)

var lemContract = contract.MustLoad()

var (
	maxFileSizeBytes   = lemContract.Collection.MaxFileBytes
	maxUploadFileBytes = min(maxFileSizeBytes, lemContract.ServerLimitsMirrored.MaxFileBytesServer)
	maxOutputBytes     = lemContract.Commands.OutputCapBytes
	maxBackoff         = lemContract.BackoffMax()
	agentVersion       = lemContract.AgentVersion
)

var httpClient = &http.Client{Timeout: lemContract.HTTPTimeout()}

type agentState struct {
	DeviceID             string            `json:"device_id,omitempty"`
	DeviceToken          string            `json:"device_token,omitempty"`
	HardwareFingerprint  string            `json:"hardware_fingerprint,omitempty"`
	LastSyncHashes       map[string]string `json:"last_sync_hashes"`
	LastSaveSyncHashes   map[string]string `json:"last_save_sync_hashes"`
	LastPreferenceSync   time.Time         `json:"last_preference_sync"`
	LastAppInventorySync time.Time         `json:"last_app_inventory_sync"`
	LastSaveSync         time.Time         `json:"last_save_sync"`
}

type preferencePayload struct {
	Category     string `json:"category"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
	Encoding     string `json:"encoding,omitempty"`
}

type preferenceSyncResponse struct {
	Saved    []preferencePayload `json:"saved"`
	Rejected []struct {
		Filename string `json:"filename"`
		Reason   string `json:"reason"`
	} `json:"rejected"`
}

func (st *agentState) save() {
	saveAgentState(st)
}

func (st *agentState) pruneSyncHashes(keep map[string]bool) {
	pruneSyncHashes(st, keep)
}

func stateFilePath() string {
	return contract.StatePath()
}

func loadAgentState() *agentState {
	state := &agentState{LastSyncHashes: map[string]string{}, LastSaveSyncHashes: map[string]string{}}
	data, err := os.ReadFile(stateFilePath())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, state); err != nil {
		log.Printf("discarding corrupt agent state %s: %v", stateFilePath(), err)
		return &agentState{LastSyncHashes: map[string]string{}, LastSaveSyncHashes: map[string]string{}}
	}
	if state.LastSyncHashes == nil {
		state.LastSyncHashes = map[string]string{}
	}
	if state.LastSaveSyncHashes == nil {
		state.LastSaveSyncHashes = map[string]string{}
	}
	return state
}

func pruneSyncHashes(st *agentState, keep map[string]bool) {
	for path := range st.LastSyncHashes {
		if !keep[path] {
			delete(st.LastSyncHashes, path)
		}
	}
}

func saveAgentState(st *agentState) {
	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("failed to create state dir: %v", err)
		return
	}
	payload, err := json.Marshal(st)
	if err != nil {
		log.Printf("failed to marshal agent state: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		log.Printf("failed to write state file: %v", err)
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("failed to rename state file: %v", err)
		os.Remove(tmp)
	}
}

func newCommandID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "dreq-" + hex.EncodeToString(b)
}

type dockerRequest struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Payload string `json:"payload,omitempty"`
}

type dockerResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func processDockerRequests(serverURL, deviceID, deviceToken string) error {
	status, payload, err := getJSON(serverURL+"/api/devices/"+deviceID+"/docker/pending", deviceToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("command poll returned %d", status)
	}

	var requests []dockerRequest
	json.Unmarshal(payload, &requests)

	if len(requests) == 0 {
		return nil
	}

	for _, req := range requests {
		result := executeDockerRequest(req)

		postJSON(
			serverURL+"/api/devices/"+deviceID+"/docker/result",
			deviceToken,
			map[string]string{
				"device_token": deviceToken,
				"request_id":   req.ID,
				"status":       result.Status,
				"message":      result.Message,
			},
		)
	}
	return nil
}

func dockerRequestAllowed(policy localPolicy, reqType string) bool {
	switch reqType {
	case "list", "stats", "logs":
		return policy.AllowDockerRead
	case "start", "stop", "restart", "kill", "remove":
		return policy.AllowDockerLifecycle
	case "exec":
		return policy.AllowDockerExec
	case "prune_system", "prune_image", "prune_container", "prune_network":
		return policy.AllowDockerPrune
	case "compose_read", "compose_write", "compose_up", "compose_down", "compose_ps", "compose_logs":
		return policy.AllowDockerCompose
	default:
		return false
	}
}

func executeDockerRequest(req dockerRequest) dockerResult {
	policy := loadLocalPolicy()
	if !dockerRequestAllowed(policy, req.Type) {
		return dockerResult{"failed", "command disabled by local policy"}
	}
	if !collectors.DockerIsAvailable() {
		return dockerResult{"failed", "docker not available"}
	}

	switch req.Type {
	case "list":
		containers, err := collectors.DockerListContainers(true)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		data, err := json.Marshal(containers)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", string(data)}

	case "stats":
		stats, err := collectors.DockerContainerStats(req.Target)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		data, err := json.Marshal(stats)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", string(data)}

	case "logs":
		tail := 200
		if req.Payload != "" {
			if n, err := strconv.Atoi(req.Payload); err == nil && n > 0 {
				tail = n
			}
		}
		logs, err := collectors.DockerContainerLogs(req.Target, tail)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", logs}

	case "start", "stop", "restart", "kill":
		err := collectors.DockerContainerAction(req.Target, req.Type)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", ""}

	case "remove":
		err := collectors.DockerContainerRemove(req.Target, true)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", ""}

	case "exec":
		var cmd []string
		if err := json.Unmarshal([]byte(req.Payload), &cmd); err != nil {
			return dockerResult{"failed", "invalid exec command"}
		}
		execID, err := collectors.DockerExecCreate(req.Target, cmd)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		output, err := collectors.DockerExecStart(execID)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", output}

	case "compose_read":
		files, err := collectors.DockerComposeFiles()
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		data, err := json.Marshal(files)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", string(data)}

	case "compose_write":
		err := collectors.DockerComposeWrite(req.Target, req.Payload)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", ""}

	case "compose_up":
		output, err := collectors.DockerComposeUp(req.Target)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", output}

	case "compose_down":
		output, err := collectors.DockerComposeDown(req.Target)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", output}

	case "compose_ps":
		output, err := collectors.DockerComposePs(req.Target)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", output}

	case "compose_logs":
		output, err := collectors.DockerComposeLogs(req.Target)
		if err != nil {
			return dockerResult{"failed", err.Error()}
		}
		return dockerResult{"completed", output}

	case "prune_system", "prune_image", "prune_container", "prune_network":
		switch req.Type {
		case "prune_system":
			output, err := collectors.DockerSystemPrune()
			if err != nil {
				return dockerResult{"failed", err.Error()}
			}
			return dockerResult{"completed", output}
		case "prune_image":
			output, err := collectors.DockerImagePrune()
			if err != nil {
				return dockerResult{"failed", err.Error()}
			}
			return dockerResult{"completed", output}
		case "prune_container":
			output, err := collectors.DockerContainerPrune()
			if err != nil {
				return dockerResult{"failed", err.Error()}
			}
			return dockerResult{"completed", output}
		case "prune_network":
			output, err := collectors.DockerNetworkPrune()
			if err != nil {
				return dockerResult{"failed", err.Error()}
			}
			return dockerResult{"completed", output}
		}
	}

	return dockerResult{"failed", "unknown request type"}
}

type telemetryStats struct {
	CPUUsagePercent     float64           `json:"cpu_usage_percent"`
	MemoryUsedBytes     uint64            `json:"memory_used_bytes"`
	MemoryTotalBytes    uint64            `json:"memory_total_bytes"`
	CPUTemperature      float64           `json:"cpu_temperature"`
	GPUTemperature      float64           `json:"gpu_temperature_celsius,omitempty"`
	PowerWatts          float64           `json:"power_watts"`
	BatteryPercent      float64           `json:"battery_percent,omitempty"`
	BatteryStatus       string            `json:"battery_status,omitempty"`
	AgentCPUUsage       float64           `json:"agent_cpu_usage"`
	AgentMemoryBytes    uint64            `json:"agent_memory_bytes,omitempty"`
	AgentVersion        string            `json:"agent_version"`
	OperatingSystem     string            `json:"operating_system"`
	Architecture        string            `json:"architecture"`
	CPUModel            string            `json:"cpu_model,omitempty"`
	KernelVersion       string            `json:"kernel_version"`
	DesktopEnvironment  string            `json:"desktop_environment"`
	Locale              string            `json:"locale"`
	Timezone            string            `json:"timezone,omitempty"`
	BootTime            string            `json:"boot_time"`
	UptimeSeconds       int64             `json:"uptime_seconds"`
	LoadAverage         string            `json:"load_average,omitempty"`
	NetworkIFaces       []networkIface    `json:"network_ifaces,omitempty"`
	DiskReadBytes       uint64            `json:"disk_read_bytes,omitempty"`
	DiskWriteBytes      uint64            `json:"disk_write_bytes,omitempty"`
	DiskReadRate        float64           `json:"disk_read_rate,omitempty"`
	DiskWriteRate       float64           `json:"disk_write_rate,omitempty"`
	DiskPartitions      []diskPartition   `json:"disk_partitions,omitempty"`
	SwapUsedBytes       uint64            `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes      uint64            `json:"swap_total_bytes,omitempty"`
	MemoryBuffersBytes  uint64            `json:"memory_buffers_bytes,omitempty"`
	MemoryCachedBytes   uint64            `json:"memory_cached_bytes,omitempty"`
	CPUCoreUsage        []float64         `json:"cpu_core_usage,omitempty"`
	TopCPUProcesses     []processInfo     `json:"top_cpu_processes,omitempty"`
	TopMemProcesses     []processInfo     `json:"top_mem_processes,omitempty"`
	DockerAvailable     bool              `json:"docker_available"`
	DockerContainers    []dockerContainer `json:"docker_containers,omitempty"`
	DockerInfo          *dockerInfo       `json:"docker_info,omitempty"`
	LynisAvailable      bool              `json:"lynis_available"`
	LynisInstallCmd     string            `json:"lynis_install_cmd,omitempty"`
	Logs                []deviceLog       `json:"logs,omitempty"`
	HardwareFingerprint string            `json:"hardware_fingerprint,omitempty"`
	CollectedAt         string            `json:"collected_at,omitempty"`
}

type diskPartition struct {
	Mount       string  `json:"mount"`
	Device      string  `json:"device"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type processInfo struct {
	PID         int     `json:"pid"`
	Name        string  `json:"name"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemRSSBytes uint64  `json:"mem_rss_bytes"`
}

type deviceLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

// toDeviceLogs converts collector log entries into the telemetry payload shape.
func toDeviceLogs(logs []collectors.DeviceLog) []deviceLog {
	if len(logs) == 0 {
		return nil
	}
	out := make([]deviceLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, deviceLog{
			Timestamp: l.Timestamp,
			Level:     l.Level,
			Source:    l.Source,
			Message:   l.Message,
		})
	}
	return out
}

type networkIface struct {
	Name      string  `json:"name"`
	RXBytes   uint64  `json:"rx_bytes"`
	TXBytes   uint64  `json:"tx_bytes"`
	RXRate    float64 `json:"rx_rate"`
	TXRate    float64 `json:"tx_rate"`
	RXPackets uint64  `json:"rx_packets"`
	TXPackets uint64  `json:"tx_packets"`
	RXErrors  uint64  `json:"rx_errors"`
	TXErrors  uint64  `json:"tx_errors"`
}

type dockerContainer struct {
	ID     string   `json:"id"`
	Names  []string `json:"names,omitempty"`
	Name   string   `json:"name"`
	Image  string   `json:"image"`
	State  string   `json:"state"`
	Status string   `json:"status,omitempty"`
}

type dockerInfo struct {
	Version string `json:"version"`
	Total   int    `json:"total"`
	Running int    `json:"running"`
	Stopped int    `json:"stopped"`
	Paused  int    `json:"paused"`
	Images  int    `json:"images"`
	Driver  string `json:"driver"`
	NCPU    int    `json:"ncpu"`
}

type reconnectRequest struct {
	Fingerprint string `json:"fingerprint"`
	Hostname    string `json:"hostname"`
}

type reconnectResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	Reconnected bool   `json:"reconnected"`
}

// tryReconnect attempts to recover device credentials from the server
// using the hardware fingerprint. Returns the device ID, token, and
// whether reconnection was successful.
func tryReconnect(serverURL, fingerprint string) (string, string, bool) {
	hostname, _ := os.Hostname()
	req := reconnectRequest{Fingerprint: fingerprint, Hostname: hostname}
	respBody, err := postJSONWithResponse(serverURL+"/api/agent/reconnect", "", req)
	if err != nil {
		log.Printf("reconnect failed: %v", err)
		return "", "", false
	}
	var resp reconnectResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		log.Printf("reconnect decode failed: %v", err)
		return "", "", false
	}
	return resp.DeviceID, resp.DeviceToken, resp.Reconnected
}

func cmdDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	serverURL := fs.String("server", "http://localhost:8080", "server URL")
	deviceID := fs.String("device-id", "", "device ID")
	deviceToken := fs.String("device-token", "", "device token")
	interval := fs.Duration("interval", 10*time.Second, "telemetry interval")
	preferenceInterval := fs.Duration("preference-interval", time.Duration(lemContract.PreferencesSync.IntervalSecondsDefault)*time.Second, "preference sync interval")
	appsInterval := fs.Duration("apps-interval", time.Duration(lemContract.AppsInventory.RefreshIntervalSeconds)*time.Second, "application inventory interval")
	savesInterval := fs.Duration("saves-interval", time.Duration(lemContract.PreferencesSync.IntervalSecondsDefault)*time.Second, "save game sync interval")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *preferenceInterval <= 0 {
		*preferenceInterval = time.Duration(lemContract.PreferencesSync.IntervalSecondsDefault) * time.Second
	}
	if *appsInterval <= 0 {
		*appsInterval = time.Duration(lemContract.AppsInventory.RefreshIntervalSeconds) * time.Second
	}
	if *savesInterval <= 0 {
		*savesInterval = time.Duration(lemContract.PreferencesSync.IntervalSecondsDefault) * time.Second
	}

	state := loadAgentState()

	// If credentials not provided via flags, load from state file.
	if strings.TrimSpace(*deviceID) == "" {
		*deviceID = state.DeviceID
	}
	if strings.TrimSpace(*deviceToken) == "" {
		*deviceToken = state.DeviceToken
	}

	// Collect hardware fingerprint for device identification.
	hwID := collectors.CollectHardwareID()

	// Try reconnect if we have a fingerprint but no valid credentials.
	if hwID.Fingerprint != "" && (*deviceID == "" || *deviceToken == "") {
		if recoveredID, recoveredToken, ok := tryReconnect(*serverURL, hwID.Fingerprint); ok {
			log.Printf("reconnected to existing device id=%s", recoveredID)
			*deviceID = recoveredID
			*deviceToken = recoveredToken
			state.DeviceID = recoveredID
			state.DeviceToken = recoveredToken
			state.HardwareFingerprint = hwID.Fingerprint
			saveAgentState(state)
		}
	}

	if strings.TrimSpace(*deviceID) == "" || strings.TrimSpace(*deviceToken) == "" {
		log.Fatal("device-id and device-token are required (or use reconnect via fingerprint)")
	}

	// Save fingerprint as metadata only; it is not a recovery credential.
	if state.HardwareFingerprint == "" && hwID.Fingerprint != "" {
		state.HardwareFingerprint = hwID.Fingerprint
		saveAgentState(state)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var previousCPU, previousIdle, previousDiskRead, previousDiskWrite uint64
	var previousNetRX, previousNetTX map[string]uint64
	var previousAt time.Time
	var agentImpact collectors.AgentImpact
	var previousCPUCores []uint64
	consecutiveFailures := 0
	// Logs are heavier to collect (journalctl), so sample them less often
	// than hardware telemetry: roughly every 60s at the default 10s interval.
	const logCollectCycles = 6
	logCycle := 0
	lastStateJSON := ""
	lastPreferenceAttempt := time.Time{}
	lastAppInventoryAttempt := time.Time{}
	lastSaveAttempt := time.Time{}

	for {
		now := time.Now()
		log.Printf("cycle started device=%s failures=%d", *deviceID, consecutiveFailures)
		hadError := false

		if lastPreferenceAttempt.IsZero() || now.Sub(lastPreferenceAttempt) >= *preferenceInterval {
			lastPreferenceAttempt = now
			if err := syncPreferences(*serverURL, *deviceID, *deviceToken, state); err != nil {
				log.Printf("preference sync failed: %v", err)
				hadError = true
			}
		}

		if lastAppInventoryAttempt.IsZero() || now.Sub(lastAppInventoryAttempt) >= *appsInterval {
			lastAppInventoryAttempt = now
			apps, err := collectors.CollectApps(lemContract.AppsInventory.Sources)
			if err != nil {
				log.Printf("application inventory collection failed: %v", err)
				hadError = true
			} else if err := sendAppInventory(*serverURL, *deviceID, *deviceToken, state, apps); err != nil {
				log.Printf("application inventory sync failed: %v", err)
				hadError = true
			}
		}

		if lastSaveAttempt.IsZero() || now.Sub(lastSaveAttempt) >= *savesInterval {
			lastSaveAttempt = now
			if err := syncSaves(*serverURL, *deviceID, *deviceToken, state); err != nil {
				log.Printf("save sync failed: %v", err)
				hadError = true
			}
		}

		if collectors.DockerIsAvailable() {
			if err := processDockerRequests(*serverURL, *deviceID, *deviceToken); err != nil {
				log.Printf("docker request processing failed: %v", err)
				hadError = true
			}
		}

		if err := processCommands(*serverURL, *deviceID, *deviceToken); err != nil {
			log.Printf("command processing failed: %v", err)
			hadError = true
		}
		stats, cpuTotal, idle, diskRead, diskWrite, newNetRX, newNetTX, newImpact, newCores, err := collectHardwareStats(previousCPU, previousIdle, previousDiskRead, previousDiskWrite, previousNetRX, previousNetTX, previousAt, now, agentImpact, previousCPUCores)
		agentImpact = newImpact
		previousCPUCores = newCores
		if err == nil {
			logCycle++
			if logCycle >= logCollectCycles {
				logCycle = 0
				stats.Logs = toDeviceLogs(collectors.CollectDeviceLogs())
			}
			err = sendTelemetry(*serverURL, *deviceID, *deviceToken, stats)
		}
		if err != nil {
			log.Printf("telemetry failed: %v", err)
			hadError = true
		} else {
			log.Printf("telemetry sent cpu=%.1f%% memory=%d/%d power=%.1fW agent=%.1f%% os=%q", stats.CPUUsagePercent, stats.MemoryUsedBytes, stats.MemoryTotalBytes, stats.PowerWatts, stats.AgentCPUUsage, stats.OperatingSystem)
		}
		previousCPU, previousIdle, previousDiskRead, previousDiskWrite, previousAt = cpuTotal, idle, diskRead, diskWrite, now
		previousNetRX, previousNetTX = newNetRX, newNetTX
		if serialized, err := json.Marshal(state); err == nil && string(serialized) != lastStateJSON {
			saveAgentState(state)
			lastStateJSON = string(serialized)
		}

		wait := *interval
		if hadError {
			consecutiveFailures++
			if backoff := wait << uint(min(consecutiveFailures-1, 6)); backoff < maxBackoff {
				wait = backoff
			} else {
				wait = maxBackoff
			}
			jitter := time.Duration(rand.Int63n(int64(wait.Seconds()*lemContract.Resilience.JitterFraction*float64(time.Second)) + 1))
			wait += jitter
			log.Printf("cycle had failures (%d consecutive), next attempt in %s", consecutiveFailures, wait)
		} else if consecutiveFailures > 0 {
			consecutiveFailures = 0
			log.Printf("connection recovered")
		}
		select {
		case <-stop:
			log.Printf("shutdown signal received, stopping daemon")
			return
		case <-time.After(wait):
		}
	}
}

func getJSON(url, token string) (int, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

func postJSON(url, token string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("POST %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}
	return nil
}

func postJSONWithResponse(url, token string, data any) ([]byte, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("POST %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}
	return respBody, err
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: agent <command> [args...]")
	}
	switch os.Args[1] {
	case "daemon":
		cmdDaemon(os.Args[2:])
	case "update":
		if err := cmdUpdate(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "--version", "version":
		fmt.Println(agentVersion)
	default:
		log.Fatalf("unknown command: %s", os.Args[1])
	}
}

// processCommands polls the server for pending commands and executes them.
func processCommands(serverURL, deviceID, deviceToken string) error {
	status, payload, err := getJSON(serverURL+"/api/devices/"+deviceID+"/commands", deviceToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("command poll returned %d", status)
	}

	var commands []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Path    string `json:"path"`
		Name    string `json:"name"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(payload, &commands); err != nil {
		return fmt.Errorf("unmarshal commands: %w", err)
	}

	for _, cmd := range commands {
		result := executeCommand(cmd)
		resultURL := serverURL + "/api/devices/" + deviceID + "/commands/" + cmd.ID
		if err := postJSON(resultURL, deviceToken, map[string]string{
			"device_token": deviceToken,
			"status":       result.Status,
			"message":      result.Message,
		}); err != nil {
			return err
		}
	}
	return nil
}

type commandResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func executeCommand(cmd struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Payload string `json:"payload"`
}) commandResult {
	policy := loadLocalPolicy()
	switch cmd.Type {
	case "exclude_file":
		if !policy.AllowExcludeFile {
			return commandResult{"failed", "command disabled by local policy"}
		}
		if err := excludeFile(cmd.Path); err != nil {
			return commandResult{"failed", err.Error()}
		}
		return commandResult{"completed", ""}
	case "restore_saves":
		if !policy.AllowRestoreSaves {
			return commandResult{"failed", "command disabled by local policy"}
		}
		count, err := restoreSaves(cmd.Payload)
		if err != nil {
			return commandResult{"failed", err.Error()}
		}
		return commandResult{"completed", fmt.Sprintf("restored %d save files", count)}
	case "lynis_audit":
		if !policy.AllowLynisAudit {
			return commandResult{"failed", "command disabled by local policy"}
		}
		timeout := lemContract.LynisTimeout()
		if policy.CommandTimeoutSeconds > 0 {
			policyTimeout := time.Duration(policy.CommandTimeoutSeconds) * time.Second
			if policyTimeout < timeout {
				timeout = policyTimeout
			}
		}
		return executeLynisAuditWithTimeout(timeout)
	case "install_app":
		if !policy.AllowInstallApp {
			return commandResult{"failed", "command disabled by local policy"}
		}
		return commandResult{"failed", "install_app is not implemented"}
	default:
		return commandResult{"failed", "unsupported command type: " + cmd.Type}
	}
}

// lynisReport represents the structured output of a Lynis security audit.
type lynisReport struct {
	HardeningIndex   int             `json:"hardening_index"`
	TotalWarnings    int             `json:"total_warnings"`
	TotalSuggestions int             `json:"total_suggestions"`
	TotalTests       int             `json:"total_tests"`
	TestsPassed      int             `json:"tests_passed"`
	LynisVersion     string          `json:"lynis_version"`
	OS               string          `json:"os"`
	Kernel           string          `json:"kernel"`
	Warnings         []lynisFinding  `json:"warnings"`
	Suggestions      []lynisFinding  `json:"suggestions"`
	Categories       []lynisCategory `json:"categories"`
	AuditDate        string          `json:"audit_date"`
}

type lynisFinding struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Severity    string `json:"severity,omitempty"`
}

type lynisCategory struct {
	Name        string `json:"name"`
	Tests       int    `json:"tests"`
	Passed      int    `json:"passed"`
	Warnings    int    `json:"warnings"`
	Suggestions int    `json:"suggestions"`
}

// executeLynisAudit runs Lynis with the contract timeout.
func executeLynisAudit() commandResult {
	return executeLynisAuditWithTimeout(lemContract.LynisTimeout())
}

func executeLynisAuditWithTimeout(timeout time.Duration) commandResult {

	// Check if Lynis is installed
	lynisPath, err := exec.LookPath("lynis")
	if err != nil {
		return commandResult{
			"failed",
			"Lynis is not installed. Install it with: sudo apt install lynis (Debian/Ubuntu) or sudo pacman -S lynis (Arch)",
		}
	}

	// Create temp files for report and log
	reportFile := "/tmp/lem-lynis-report.dat"
	logFile := "/tmp/lem-lynis.log"

	// Run Lynis audit
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, lynisPath, "audit", "system", "--cronjob", "--no-colors",
		"--report-file", reportFile, "--logfile", logFile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return commandResult{"failed", fmt.Sprintf("Lynis audit timed out after %s", timeout)}
		}
		// Lynis may return non-zero on warnings, still try to parse report
	}

	// Read the report file
	reportData, err := os.ReadFile(reportFile)
	if err != nil {
		return commandResult{"failed", fmt.Sprintf("failed to read Lynis report: %v", err)}
	}
	if len(reportData) > lemContract.SecurityAudit.ReportMaxBytes {
		return commandResult{"failed", "Lynis report exceeds the configured size limit"}
	}

	report := parseLynisReport(string(reportData))

	// Get Lynis version
	if out, err := exec.Command(lynisPath, "show", "version").Output(); err == nil {
		report.LynisVersion = strings.TrimSpace(string(out))
	}

	// Get OS info
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				report.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	report.Kernel = readKernelVersion()
	report.AuditDate = time.Now().UTC().Format(time.RFC3339)

	// Marshal report to JSON
	data, err := json.Marshal(report)
	if err != nil {
		return commandResult{"failed", fmt.Sprintf("failed to marshal report: %v", err)}
	}

	// Cleanup temp files
	os.Remove(reportFile)
	os.Remove(logFile)

	return commandResult{"completed", string(data)}
}

// isArchLinux detects if the system is Arch-based.
func isArchLinux() bool {
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		content := string(data)
		if strings.Contains(strings.ToLower(content), "arch") || strings.Contains(strings.ToLower(content), "manjaro") {
			return true
		}
	}
	if _, err := os.Stat("/etc/arch-release"); err == nil {
		return true
	}
	return false
}

// parseLynisReport parses the key=value format of lynis-report.dat.
func parseLynisReport(raw string) lynisReport {
	report := lynisReport{}
	warnings := map[string]*lynisFinding{}
	suggestions := map[string]*lynisFinding{}
	categories := map[string]*lynisCategory{}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := parts[1]

		switch {
		case key == "hardening_index":
			report.HardeningIndex, _ = strconv.Atoi(val)

		case strings.HasPrefix(key, "warning["):
			// Format: warning[ID][]=description
			idEnd := strings.Index(key, "]")
			idStart := strings.Index(key, "[")
			if idStart >= 0 && idEnd > idStart {
				id := key[idStart+1 : idEnd]
				cat := ""
				if bracketEnd := strings.Index(key[idEnd+1:], "["); bracketEnd >= 0 {
					catStart := idEnd + 1 + bracketEnd + 1
					catEnd := strings.Index(key[catStart:], "]")
					if catEnd >= 0 {
						cat = key[catStart : catStart+catEnd]
					}
				}
				if _, exists := warnings[id]; !exists {
					warnings[id] = &lynisFinding{ID: id, Category: cat, Description: val, Severity: "warning"}
				}
			}

		case strings.HasPrefix(key, "suggestion["):
			// Format: suggestion[ID][]=description
			idEnd := strings.Index(key, "]")
			idStart := strings.Index(key, "[")
			if idStart >= 0 && idEnd > idStart {
				id := key[idStart+1 : idEnd]
				cat := ""
				if bracketEnd := strings.Index(key[idEnd+1:], "["); bracketEnd >= 0 {
					catStart := idEnd + 1 + bracketEnd + 1
					catEnd := strings.Index(key[catStart:], "]")
					if catEnd >= 0 {
						cat = key[catStart : catStart+catEnd]
					}
				}
				if _, exists := suggestions[id]; !exists {
					suggestions[id] = &lynisFinding{ID: id, Category: cat, Description: val}
				}
			}

		case strings.HasPrefix(key, "tests_group[]"):
			// Format: tests_group[]=category_name
			if _, exists := categories[val]; !exists {
				categories[val] = &lynisCategory{Name: val}
			}

		case strings.HasPrefix(key, "tests_warnings[]"):
			// Format: tests_warnings[]=category_name
			if cat, ok := categories[val]; ok {
				cat.Warnings++
			} else {
				categories[val] = &lynisCategory{Name: val, Warnings: 1}
			}

		case strings.HasPrefix(key, "tests_suggestions[]"):
			// Format: tests_suggestions[]=category_name
			if cat, ok := categories[val]; ok {
				cat.Suggestions++
			} else {
				categories[val] = &lynisCategory{Name: val, Suggestions: 1}
			}

		case strings.HasPrefix(key, "tests_passed[]"):
			// Format: tests_passed[]=category_name
			if cat, ok := categories[val]; ok {
				cat.Passed++
			} else {
				categories[val] = &lynisCategory{Name: val, Passed: 1}
			}

		case strings.HasPrefix(key, "tests_total[]"):
			// Format: tests_total[]=category_name
			if cat, ok := categories[val]; ok {
				cat.Tests++
			} else {
				categories[val] = &lynisCategory{Name: val, Tests: 1}
			}
		}
	}

	// Convert maps to slices
	for _, w := range warnings {
		report.Warnings = append(report.Warnings, *w)
	}
	for _, s := range suggestions {
		report.Suggestions = append(report.Suggestions, *s)
	}
	for _, c := range categories {
		report.Categories = append(report.Categories, *c)
	}

	report.TotalWarnings = len(report.Warnings)
	report.TotalSuggestions = len(report.Suggestions)

	// Count totals from categories
	for _, c := range report.Categories {
		report.TotalTests += c.Tests
		report.TestsPassed += c.Passed
	}

	return report
}

// collectHardwareStats collects real hardware data from /proc, /sys, and collectors.
// Independent data sources are collected in parallel using goroutines.
func collectHardwareStats(prevCPU, prevIdle, prevDiskRead, prevDiskWrite uint64, prevNetRX, prevNetTX map[string]uint64, prevAt, now time.Time, agentImpact collectors.AgentImpact, prevCPUCores []uint64) (telemetryStats, uint64, uint64, uint64, uint64, map[string]uint64, map[string]uint64, collectors.AgentImpact, []uint64, error) {
	var stats telemetryStats

	// CPU counters must be read first (needed for delta calculation)
	cpuTotal, idle, err := readCPUCounters()
	if err != nil {
		return stats, 0, 0, 0, 0, nil, nil, agentImpact, nil, fmt.Errorf("read cpu: %w", err)
	}
	if prevCPU > 0 && cpuTotal > prevCPU {
		totalDelta := cpuTotal - prevCPU
		idleDelta := idle - prevIdle
		if totalDelta > 0 {
			stats.CPUUsagePercent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
		}
	}

	// Memory is fast, read synchronously
	memUsed, memTotal, memErr := readMemoryInfo()
	if memErr == nil {
		stats.MemoryUsedBytes = memUsed
		stats.MemoryTotalBytes = memTotal
	}

	// Static info (no I/O, instant)
	stats.OperatingSystem = runtime.GOOS
	stats.Architecture = runtime.GOARCH
	stats.AgentVersion = agentVersion
	stats.CollectedAt = now.Format(time.RFC3339)

	// Parallel collectors for heavier I/O operations
	var wg sync.WaitGroup
	var mu sync.Mutex

	// CPU cores + core usage
	var newCPUCores []uint64
	var corePercents []float64
	wg.Add(1)
	go func() {
		defer wg.Done()
		newCPUCores, corePercents = collectors.CollectCPUCores(prevCPUCores)
	}()

	// CPU temperature
	var temp float64
	wg.Add(1)
	go func() {
		defer wg.Done()
		temp = collectors.CollectCPUTemperature()
	}()

	// GPU temperature
	var gpuTemp float64
	wg.Add(1)
	go func() {
		defer wg.Done()
		gpuTemp = collectors.CollectGPUTemperature()
	}()

	// Battery (percent + status)
	var batteryPercent float64
	var batteryStatus string
	wg.Add(1)
	go func() {
		defer wg.Done()
		batteryPercent, batteryStatus = collectors.CollectBattery()
	}()

	// Instantaneous power draw (watts), independent from battery percentage
	var powerWatts float64
	wg.Add(1)
	go func() {
		defer wg.Done()
		powerWatts = collectors.CollectPowerWatts()
	}()

	// Agent impact
	var newImpact collectors.AgentImpact
	wg.Add(1)
	go func() {
		defer wg.Done()
		newImpact = collectors.CollectAgentImpact(agentImpact)
	}()

	// Network
	var interfaces []collectors.NetworkInterface
	var newNetRX, newNetTX map[string]uint64
	wg.Add(1)
	go func() {
		defer wg.Done()
		interfaces = collectors.CollectNetwork()
		newNetRX = map[string]uint64{}
		newNetTX = map[string]uint64{}
		for _, iface := range interfaces {
			newNetRX[iface.Name] = iface.RXBytes
			newNetTX[iface.Name] = iface.TXBytes
		}
	}()

	// Disk I/O
	var diskRead, diskWrite uint64
	wg.Add(1)
	go func() {
		defer wg.Done()
		diskRead, diskWrite, _ = readDiskIO()
	}()

	// Disk partitions
	var diskParts []collectors.DiskPartition
	wg.Add(1)
	go func() {
		defer wg.Done()
		diskParts = collectors.CollectDiskPartitions()
	}()

	// Memory expanded (swap, buffers, cached)
	var memExpanded collectors.MemoryExpanded
	wg.Add(1)
	go func() {
		defer wg.Done()
		memExpanded = collectors.CollectMemoryExpanded()
	}()

	// Top processes
	var topCPU, topMem []collectors.ProcessInfo
	wg.Add(1)
	go func() {
		defer wg.Done()
		topCPU, topMem = collectors.CollectTopProcesses(10, runtime.NumCPU())
	}()

	// System info (file reads)
	var kernel, desktop, locale, bootTime, cpuModel, loadAvg, tz string
	var uptime float64
	wg.Add(1)
	go func() {
		defer wg.Done()
		kernel = readKernelVersion()
		desktop = detectDesktopEnvironment()
		locale = readLocale()
		uptime, bootTime = readUptime()
		cpuModel = readCPUModel()
		loadAvg = readLoadAverage()
		tz = readTimezone()
	}()

	// Agent memory
	var agentMem uint64
	wg.Add(1)
	go func() {
		defer wg.Done()
		agentMem = readAgentMemory()
	}()

	// Docker (potentially slow socket communication)
	var dockerAvailable bool
	var dockerContainers []dockerContainer
	var dockerInf *dockerInfo
	wg.Add(1)
	go func() {
		defer wg.Done()
		dockerAvailable = collectors.DockerIsAvailable()
		if dockerAvailable {
			if containers, err := collectors.DockerListContainers(false); err == nil {
				var dc []dockerContainer
				for _, c := range containers {
					name := ""
					if len(c.Names) > 0 {
						name = strings.TrimPrefix(c.Names[0], "/")
					}
					dc = append(dc, dockerContainer{
						ID:     c.ID,
						Names:  c.Names,
						Name:   name,
						Image:  c.Image,
						State:  c.State,
						Status: c.Status,
					})
				}
				mu.Lock()
				dockerContainers = dc
				mu.Unlock()
			}
			if info, err := collectors.GetDockerInfo(); err == nil {
				dockerInf = &dockerInfo{
					Version: info.ServerVersion,
					Total:   info.ContainersTotal,
					Running: info.ContainersRunning,
					Stopped: info.ContainersStopped,
					Paused:  info.ContainersPaused,
					Images:  info.ImagesCount,
					Driver:  info.Driver,
					NCPU:    info.NCPU,
				}
			}
		}
	}()

	// Hardware fingerprint (stable identifiers for device deduplication)
	var hwID collectors.HardwareID
	wg.Add(1)
	go func() {
		defer wg.Done()
		hwID = collectors.CollectHardwareID()
	}()

	wg.Wait()

	// Merge results
	stats.CPUTemperature = temp
	stats.GPUTemperature = gpuTemp
	stats.PowerWatts = powerWatts
	stats.BatteryPercent = batteryPercent
	stats.BatteryStatus = batteryStatus
	stats.AgentCPUUsage = newImpact.CPUPercent
	stats.AgentMemoryBytes = agentMem
	stats.KernelVersion = kernel
	stats.DesktopEnvironment = desktop
	stats.Locale = locale
	stats.Timezone = tz
	stats.UptimeSeconds = int64(uptime)
	stats.BootTime = bootTime
	stats.LoadAverage = loadAvg
	stats.CPUModel = cpuModel
	stats.CPUCoreUsage = corePercents
	stats.DockerAvailable = dockerAvailable
	stats.DockerContainers = dockerContainers
	stats.DockerInfo = dockerInf
	stats.HardwareFingerprint = hwID.Fingerprint

	// Lynis availability
	if path, err := exec.LookPath("lynis"); err == nil {
		stats.LynisAvailable = true
		log.Printf("lynis found at %s", path)
	} else if _, err := os.Stat("/usr/bin/lynis"); err == nil {
		stats.LynisAvailable = true
		log.Printf("lynis found at /usr/bin/lynis")
	} else if _, err := os.Stat("/usr/sbin/lynis"); err == nil {
		stats.LynisAvailable = true
		log.Printf("lynis found at /usr/sbin/lynis")
	} else {
		stats.LynisAvailable = false
		if isArchLinux() {
			stats.LynisInstallCmd = "sudo pacman -S lynis"
		} else {
			stats.LynisInstallCmd = "sudo apt install lynis"
		}
		log.Printf("lynis not found, install_cmd=%s", stats.LynisInstallCmd)
	}

	// Memory expanded
	stats.SwapUsedBytes = memExpanded.SwapUsedBytes
	stats.SwapTotalBytes = memExpanded.SwapTotalBytes
	stats.MemoryBuffersBytes = memExpanded.BuffersBytes
	stats.MemoryCachedBytes = memExpanded.CachedBytes

	// Disk I/O rates
	stats.DiskReadBytes = diskRead
	stats.DiskWriteBytes = diskWrite
	if prevDiskRead > 0 && diskRead > prevDiskRead {
		elapsed := now.Sub(prevAt).Seconds()
		if elapsed > 0 {
			stats.DiskReadRate = float64(diskRead-prevDiskRead) / elapsed
			stats.DiskWriteRate = float64(diskWrite-prevDiskWrite) / elapsed
		}
	}

	// Disk partitions
	for _, p := range diskParts {
		stats.DiskPartitions = append(stats.DiskPartitions, diskPartition{
			Mount:       p.Mount,
			Device:      p.Device,
			TotalBytes:  p.TotalBytes,
			UsedBytes:   p.UsedBytes,
			FreeBytes:   p.FreeBytes,
			UsedPercent: p.UsedPercent,
		})
	}

	// Top processes
	for _, p := range topCPU {
		stats.TopCPUProcesses = append(stats.TopCPUProcesses, processInfo{
			PID:         p.PID,
			Name:        p.Name,
			CPUPercent:  p.CPUPercent,
			MemRSSBytes: p.MemRSS,
		})
	}
	for _, p := range topMem {
		stats.TopMemProcesses = append(stats.TopMemProcesses, processInfo{
			PID:         p.PID,
			Name:        p.Name,
			CPUPercent:  p.CPUPercent,
			MemRSSBytes: p.MemRSS,
		})
	}

	// Compute network rates (needs previous state)
	newNetRXFinal := map[string]uint64{}
	newNetTXFinal := map[string]uint64{}
	var netIfaces []networkIface
	for _, iface := range interfaces {
		rxRate := 0.0
		txRate := 0.0
		if prevNetRX != nil && !prevAt.IsZero() {
			elapsed := now.Sub(prevAt).Seconds()
			if elapsed > 0 {
				if prev, ok := prevNetRX[iface.Name]; ok && iface.RXBytes > prev {
					rxRate = float64(iface.RXBytes-prev) / elapsed
				}
				if prev, ok := prevNetTX[iface.Name]; ok && iface.TXBytes > prev {
					txRate = float64(iface.TXBytes-prev) / elapsed
				}
			}
		}
		newNetRXFinal[iface.Name] = iface.RXBytes
		newNetTXFinal[iface.Name] = iface.TXBytes
		netIfaces = append(netIfaces, networkIface{
			Name:      iface.Name,
			RXBytes:   iface.RXBytes,
			TXBytes:   iface.TXBytes,
			RXRate:    rxRate,
			TXRate:    txRate,
			RXPackets: iface.RXPackets,
			TXPackets: iface.TXPackets,
			RXErrors:  iface.RXErrors,
			TXErrors:  iface.TXErrors,
		})
	}
	stats.NetworkIFaces = netIfaces

	return stats, cpuTotal, idle, diskRead, diskWrite, newNetRXFinal, newNetTXFinal, newImpact, newCPUCores, nil
}

// sendTelemetry sends collected hardware stats to the server.
func sendTelemetry(serverURL, deviceID, deviceToken string, stats telemetryStats) error {
	stats.AgentVersion = agentVersion
	url := serverURL + "/api/devices/" + deviceID + "/telemetry"
	data := map[string]any{
		"device_token": deviceToken,
		"hardware":     stats,
	}
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal telemetry: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telemetry: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telemetry returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// readCPUCounters reads aggregate CPU counters from /proc/stat.
// Returns total and idle ticks.
func readCPUCounters() (total, idle uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			for i, f := range fields[1:] {
				v, _ := strconv.ParseUint(f, 10, 64)
				total += v
				if i == 3 { // idle is the 4th field
					idle = v
				}
			}
			return
		}
	}
	return 0, 0, fmt.Errorf("cpu line not found")
}

// readMemoryInfo reads memory usage from /proc/meminfo.
func readMemoryInfo() (used, total uint64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			key := strings.TrimSuffix(fields[0], ":")
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			values[key] = val * 1024 // kB to bytes
		}
	}
	total = values["MemTotal"]
	free := values["MemFree"]
	buffers := values["Buffers"]
	cached := values["Cached"]
	used = total - free - buffers - cached
	return
}

// readDiskIO reads disk I/O counters from /proc/diskstats.
func readDiskIO() (readBytes, writeBytes uint64, err error) {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		// Skip partitions (only sum whole disks like sda, vda, nvme0n1)
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "dm-") {
			continue
		}
		// fields[5] = sectors_read, fields[9] = sectors_written
		sectorsRead, _ := strconv.ParseUint(fields[5], 10, 64)
		sectorsWritten, _ := strconv.ParseUint(fields[9], 10, 64)
		readBytes += sectorsRead * 512
		writeBytes += sectorsWritten * 512
	}
	return
}

// readKernelVersion reads the kernel version from /proc/version.
func readKernelVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		return fields[2]
	}
	return "unknown"
}

// readUptime reads uptime from /proc/uptime and computes boot time.
func readUptime() (uptimeSeconds float64, bootTime string) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, ""
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, ""
	}
	uptimeSeconds, _ = strconv.ParseFloat(fields[0], 64)
	boot := time.Now().Add(-time.Duration(uptimeSeconds * float64(time.Second)))
	return uptimeSeconds, boot.Format(time.RFC3339)
}

// readCmd reads a command and returns its output as a string.
func readCmd(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detectDesktopEnvironment detects the current desktop environment.
func detectDesktopEnvironment() string {
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	if desktop != "" {
		return strings.ToLower(desktop)
	}
	// Check config files before DESKTOP_SESSION (which may be set to something else)
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		configDir := filepath.Join(home, ".config")
		entries, err := os.ReadDir(configDir)
		if err == nil {
			for _, e := range entries {
				name := strings.ToLower(e.Name())
				if name == "kdeglobals" || strings.HasPrefix(name, "kde") {
					return "kde"
				}
				if name == "gtk-3.0" || name == "gtk-4.0" {
					return "gtk"
				}
			}
		}
	}
	session := os.Getenv("DESKTOP_SESSION")
	if session != "" {
		return strings.ToLower(session)
	}
	return ""
}

// readLocale reads the system locale, stripping encoding.
func readLocale() string {
	locale := os.Getenv("LANG")
	if locale == "" {
		locale = os.Getenv("LC_ALL")
	}
	if locale == "" {
		return ""
	}
	// Strip encoding (e.g., "pt_BR.UTF-8" -> "pt_BR")
	if idx := strings.Index(locale, "."); idx > 0 {
		locale = locale[:idx]
	}
	return locale
}

func readCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func readLoadAverage() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		return fields[0] + " " + fields[1] + " " + fields[2]
	}
	return ""
}

func readTimezone() string {
	tz, err := os.ReadFile("/etc/timezone")
	if err == nil {
		return strings.TrimSpace(string(tz))
	}
	out, err := exec.Command("timedatectl", "show", "--property=Timezone", "--value").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func readAgentMemory() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

// localPolicy controls which command types the agent is allowed to execute.
type localPolicy struct {
	AllowInstallApp       bool `json:"allow_install_app"`
	AllowExcludeFile      bool `json:"allow_exclude_file"`
	AllowRestoreSaves     bool `json:"allow_restore_saves"`
	AllowLynisAudit       bool `json:"allow_lynis_audit"`
	AllowDockerRead       bool `json:"allow_docker_read"`
	AllowDockerLifecycle  bool `json:"allow_docker_lifecycle"`
	AllowDockerExec       bool `json:"allow_docker_exec"`
	AllowDockerPrune      bool `json:"allow_docker_prune"`
	AllowDockerCompose    bool `json:"allow_docker_compose"`
	CommandTimeoutSeconds int  `json:"command_timeout_seconds"`
}

// loadLocalPolicy reads the local policy file, falling back to safe defaults.
func loadLocalPolicy() localPolicy {
	defaults := lemContract.Commands.PolicyDefaults
	policy := localPolicy{
		AllowInstallApp:       defaults.AllowInstallApp,
		AllowExcludeFile:      defaults.AllowExcludeFile,
		AllowRestoreSaves:     defaults.AllowRestoreSaves,
		AllowLynisAudit:       defaults.AllowLynisAudit,
		AllowDockerRead:       defaults.AllowDockerRead,
		AllowDockerLifecycle:  defaults.AllowDockerLifecycle,
		AllowDockerExec:       defaults.AllowDockerExec,
		AllowDockerPrune:      defaults.AllowDockerPrune,
		AllowDockerCompose:    defaults.AllowDockerCompose,
		CommandTimeoutSeconds: defaults.CommandTimeoutSeconds,
	}
	if policy.CommandTimeoutSeconds <= 0 {
		policy.CommandTimeoutSeconds = lemContract.Commands.TimeoutSecondsDefault
	}
	path := contract.ExpandPath(lemContract.Commands.PolicyPath)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		return policy
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return policy
	}
	var filePolicy localPolicy
	if err := json.Unmarshal(data, &filePolicy); err != nil {
		return policy
	}
	if filePolicy.CommandTimeoutSeconds <= 0 || filePolicy.CommandTimeoutSeconds > 86400 {
		filePolicy.CommandTimeoutSeconds = policy.CommandTimeoutSeconds
	}
	return filePolicy
}

// installApp queues an application installation via the system package manager.
func installApp(source, name string, timeout time.Duration) error {
	safeName := strings.TrimSpace(name)
	if safeName == "" {
		return fmt.Errorf("package name is required")
	}
	if strings.ContainsAny(safeName, ";|&$`\\") {
		return fmt.Errorf("unsafe package name: %q", safeName)
	}
	if strings.Contains(safeName, "../") || strings.Contains(safeName, "..\\") {
		return fmt.Errorf("unsafe package name: %q", safeName)
	}
	switch source {
	case "apt", "pacman", "aur", "flatpak", "appimage":
	default:
		return fmt.Errorf("unsupported app source: %s", source)
	}
	return nil
}

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buf.Write(p[:remaining])
	}
	return original, nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// runCommandWithLimits executes a command with a timeout and output cap.
func runCommandWithLimits(name string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf cappedBuffer
	buf.limit = int(maxOutputBytes)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	output := buf.String()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %s", timeout)
	}
	return output, err
}

// exclusionsPath returns the path to the excluded files list.
func exclusionsPath() string {
	return contract.ExpandPath("~/.config/lem/excluded-files")
}

// excludeFile adds a path to the excluded files list.
func excludeFile(path string) error {
	// Reject traversal paths and absolute paths
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %q", path)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute path not allowed: %q", path)
	}
	excludedPath := exclusionsPath()
	if err := os.MkdirAll(filepath.Dir(excludedPath), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(excludedPath)
	if err != nil {
		data = []byte{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == path {
			return nil // already excluded
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, path...)
	data = append(data, '\n')
	return os.WriteFile(excludedPath, data, 0o644)
}

// categorizePath determines the preference category for a file path.
func categorizePath(path string) string {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "kde") || strings.Contains(lower, ".kde") {
		return "kde"
	}
	if strings.Contains(lower, "gtk-3.0") || strings.Contains(lower, "gtk-4.0") {
		return "desktop"
	}
	if strings.Contains(lower, ".bashrc") || strings.Contains(lower, ".zshrc") || strings.Contains(lower, ".profile") {
		return "shell"
	}
	if strings.Contains(lower, "code") && strings.Contains(lower, "settings.json") {
		return "app"
	}
	return "general"
}

// relativeToHome computes a relative path from home.
func relativeToHome(home, path string) string {
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

func agentHome() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

func safePreferencePath(home, path string) bool {
	if home == "" || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(home, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sensitivePreferencePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if name == "id_rsa" || name == "id_dsa" || name == "id_ecdsa" || name == "id_ed25519" || name == ".netrc" || name == ".pgpass" || name == ".my.cnf" {
		return true
	}
	return strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") || strings.HasSuffix(name, ".p12") || strings.HasSuffix(name, ".pfx")
}

// findSecret scans content for known secret patterns.
func findSecret(content string) string {
	patterns := map[string]string{
		"AKIA":        "aws_access_key_id",
		"ghp_":        "github_token",
		"xoxb-":       "slack_token",
		"sk_live_":    "stripe_live_key",
		"AIza":        "google_api_key",
		"PRIVATE KEY": "pem_private_key",
	}
	for pattern, reason := range patterns {
		if strings.Contains(content, pattern) {
			return reason
		}
	}
	return ""
}

// hashContent computes a simple hash for change detection.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// buildPreference reads a file and builds a preference payload.
func buildPreference(home, path, categoryOverride string) (preferencePayload, error) {
	if sensitivePreferencePath(path) {
		return preferencePayload{}, fmt.Errorf("sensitive filename rejected")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return preferencePayload{}, fmt.Errorf("stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return preferencePayload{}, fmt.Errorf("preference path is not a regular file")
	}
	if info.Size() > maxUploadFileBytes {
		return preferencePayload{}, fmt.Errorf("file exceeds size limit (%d > %d bytes)", info.Size(), maxUploadFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return preferencePayload{}, fmt.Errorf("read file: %w", err)
	}
	if len(data) == 0 {
		return preferencePayload{}, fmt.Errorf("empty file rejected")
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return preferencePayload{}, fmt.Errorf("binary file rejected")
	}
	content := string(data)
	if reason := findSecret(content); reason != "" {
		return preferencePayload{}, fmt.Errorf("secret detected: %s", reason)
	}
	category := categoryOverride
	if category == "" {
		category = categorizePath(path)
	}
	return preferencePayload{
		Category:     category,
		Filename:     filepath.Base(path),
		RelativePath: relativeToHome(home, path),
		Content:      content,
	}, nil
}

// scanPreferences returns changed payloads and hashes for all readable,
// explicitly selected files. It never mutates the state before upload succeeds.
func scanPreferences(paths []string, st *agentState) ([]preferencePayload, map[string]string) {
	home := agentHome()
	hashes := make(map[string]string)
	payloads := make([]preferencePayload, 0, len(paths))
	seen := make(map[string]bool)
	for _, path := range paths {
		payload, err := buildPreference(home, path, "")
		if err != nil || seen[payload.RelativePath] {
			continue
		}
		seen[payload.RelativePath] = true
		hash := hashContent(payload.Content)
		hashes[payload.RelativePath] = hash
		if st != nil && st.LastSyncHashes[payload.RelativePath] == hash {
			continue
		}
		payloads = append(payloads, payload)
	}
	return payloads, hashes
}

// collectPreferences collects preference files, skipping unchanged ones.
// The caller must commit hashes to state only after a successful server sync.
func collectPreferences(paths []string, st *agentState) []preferencePayload {
	payloads, _ := scanPreferences(paths, st)
	return payloads
}

func preferencePaths() []string {
	home := agentHome()
	candidates := append([]string(nil), lemContract.Collection.DefaultFiles...)
	if data, err := os.ReadFile(contract.ExpandPath(lemContract.Collection.ExtraAllowlistPath)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			candidates = append(candidates, line)
		}
	}

	paths := make([]string, 0, len(candidates))
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		path := contract.ExpandPath(candidate)
		if !filepath.IsAbs(path) && home != "" {
			path = filepath.Join(home, path)
		}
		path = filepath.Clean(path)
		if !safePreferencePath(home, path) || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func syncPreferences(serverURL, deviceID, deviceToken string, st *agentState) error {
	payloads, hashes := scanPreferences(preferencePaths(), st)
	if len(payloads) == 0 {
		keep := make(map[string]bool, len(hashes))
		for path := range hashes {
			keep[path] = true
		}
		st.pruneSyncHashes(keep)
		st.LastPreferenceSync = time.Now().UTC()
		st.save()
		return nil
	}

	responseBody, err := postJSONWithResponse(serverURL+"/api/devices/"+deviceID+"/sync", deviceToken, map[string]any{
		"device_token": deviceToken,
		"preferences":  payloads,
	})
	if err != nil {
		return err
	}
	var response preferenceSyncResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return fmt.Errorf("decode preference sync response: %w", err)
	}

	// Only files explicitly returned as saved may advance the local hash.
	keep := make(map[string]bool, len(hashes))
	for path := range hashes {
		keep[path] = true
	}
	st.pruneSyncHashes(keep)
	for _, saved := range response.Saved {
		if hash, ok := hashes[saved.RelativePath]; ok {
			st.LastSyncHashes[saved.RelativePath] = hash
		}
	}
	st.LastPreferenceSync = time.Now().UTC()
	st.save()
	log.Printf("preference sync completed candidates=%d saved=%d rejected=%d", len(payloads), len(response.Saved), len(response.Rejected))
	return nil
}

func sendAppInventory(serverURL, deviceID, deviceToken string, st *agentState, apps []collectors.AppInfo) error {
	if apps == nil {
		apps = []collectors.AppInfo{}
	}
	if err := postJSON(serverURL+"/api/devices/"+deviceID+"/apps", deviceToken, map[string]any{
		"device_token": deviceToken,
		"apps":         apps,
	}); err != nil {
		return err
	}
	st.LastAppInventorySync = time.Now().UTC()
	st.save()
	log.Printf("application inventory sync completed apps=%d", len(apps))
	return nil
}
