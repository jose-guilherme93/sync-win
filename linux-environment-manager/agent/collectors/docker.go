package collectors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	dockerSocketPath       = "/var/run/docker.sock"
	dockerRequestTimeout   = 30 * time.Second
	maxDockerResponseBytes = 1 << 20
	maxDockerOutputBytes   = 64 << 10
)

// DockerContainer represents a Docker container from the Engine API.
type DockerContainer struct {
	ID         string            `json:"id"`
	Names      []string          `json:"names"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
	Ports      []DockerPort      `json:"ports,omitempty"`
	Mounts     []DockerMount     `json:"mounts,omitempty"`
	CreatedAt  int64             `json:"created_at"`
	Labels     map[string]string `json:"labels,omitempty"`
	SizeRw     int64             `json:"size_rw,omitempty"`
	SizeRootFs int64             `json:"size_root_fs,omitempty"`
}

// DockerPort maps a container port to the host.
type DockerPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

// DockerMount represents a container mount point.
type DockerMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
	Name        string `json:"name,omitempty"`
}

// DockerStats holds real-time resource usage for a container.
type DockerStats struct {
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	MemoryUsage     uint64  `json:"memory_usage_bytes"`
	MemoryLimit     uint64  `json:"memory_limit_bytes"`
	MemoryPercent   float64 `json:"memory_percent"`
	NetworkRxBytes  uint64  `json:"network_rx_bytes"`
	NetworkTxBytes  uint64  `json:"network_tx_bytes"`
	BlockReadBytes  uint64  `json:"block_read_bytes"`
	BlockWriteBytes uint64  `json:"block_write_bytes"`
	PIDs            int     `json:"pids"`
}

// DockerInfo holds general Docker engine information.
type DockerInfo struct {
	ServerVersion     string `json:"server_version"`
	ContainersTotal   int    `json:"containers_total"`
	ContainersRunning int    `json:"containers_running"`
	ContainersStopped int    `json:"containers_stopped"`
	ContainersPaused  int    `json:"containers_paused"`
	ImagesCount       int    `json:"images_count"`
	Driver            string `json:"driver"`
	DockerRootDir     string `json:"docker_root_dir"`
	KernelVersion     string `json:"kernel_version"`
	OS                string `json:"os"`
	Architecture      string `json:"architecture"`
	NCPU              int    `json:"ncpu"`
	MemoryTotal       int64  `json:"memory_total_bytes"`
}

// DockerComposeFile represents a discovered compose file on the host.
type DockerComposeFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size_bytes"`
}

// dockerAPIResponse is the raw container list response from Docker Engine.
type dockerAPIContainer struct {
	ID         string            `json:"Id"`
	Names      []string          `json:"Names"`
	Image      string            `json:"Image"`
	State      string            `json:"State"`
	Status     string            `json:"Status"`
	Ports      []DockerPort      `json:"Ports"`
	Mounts     []DockerMount     `json:"Mounts"`
	Created    int64             `json:"Created"`
	Labels     map[string]string `json:"Labels"`
	SizeRw     int64             `json:"SizeRw"`
	SizeRootFs int64             `json:"SizeRootFs"`
}

// dockerStatsResponse is the raw stats response from Docker Engine.
type dockerStatsResponse struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioIOServiceBytes []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	} `json:"blkio.io_service_bytes_recursive"`
	PidsStats struct {
		Current int `json:"current"`
	} `json:"pids_stats"`
}

// dockerInfoResponse is the raw info response from Docker Engine.
type dockerInfoResponse struct {
	ServerVersion     string `json:"ServerVersion"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersStopped int    `json:"ContainersStopped"`
	ContainersPaused  int    `json:"ContainersPaused"`
	Images            int    `json:"Images"`
	Driver            string `json:"Driver"`
	DockerRootDir     string `json:"DockerRootDir"`
	KernelVersion     string `json:"KernelVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	Architecture      string `json:"Architecture"`
	NCPU              int    `json:"NCPU"`
	MemTotal          int64  `json:"MemTotal"`
}

// dockerExecCreateResponse is returned by POST /containers/{id}/exec.
type dockerExecCreateResponse struct {
	ID string `json:"Id"`
}

// DockerIsAvailable checks if the Docker socket exists and is accessible.
func DockerIsAvailable() bool {
	conn, err := net.DialTimeout("unix", dockerSocketPath, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// dockerRequest performs an HTTP request over the Docker UNIX socket.
func dockerRequest(method, path string, body interface{}) ([]byte, int, error) {
	return dockerRequestWithTimeout(method, path, body, dockerRequestTimeout)
}

func dockerRequestWithTimeout(method, path string, body interface{}, timeout time.Duration) ([]byte, int, error) {
	conn, err := net.DialTimeout("unix", dockerSocketPath, 10*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("docker socket dial: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, 0, fmt.Errorf("docker socket deadline: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, "http://localhost"+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Write request manually over the socket connection.
	if err := req.Write(conn); err != nil {
		return nil, 0, fmt.Errorf("write request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDockerResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return data, resp.StatusCode, nil
}

// DockerListContainers returns all containers (running and stopped).
func DockerListContainers(all bool) ([]DockerContainer, error) {
	path := "/containers/json"
	if all {
		path += "?all=true"
	}
	data, status, err := dockerRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("docker list containers returned %d: %s", status, string(data))
	}

	var raw []dockerAPIContainer
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal containers: %w", err)
	}

	containers := make([]DockerContainer, 0, len(raw))
	for _, c := range raw {
		containers = append(containers, DockerContainer{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			State:      c.State,
			Status:     c.Status,
			Ports:      c.Ports,
			Mounts:     c.Mounts,
			CreatedAt:  c.Created,
			Labels:     c.Labels,
			SizeRw:     c.SizeRw,
			SizeRootFs: c.SizeRootFs,
		})
	}
	return containers, nil
}

// DockerContainerStats returns resource usage stats for a single container.
func DockerContainerStats(containerID string) (*DockerStats, error) {
	if !validContainerID(containerID) {
		return nil, fmt.Errorf("invalid container ID")
	}
	data, status, err := dockerRequest("GET", "/containers/"+containerID+"/stats?stream=false", nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("docker stats returned %d: %s", status, string(data))
	}

	var raw dockerStatsResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}

	stats := &DockerStats{
		MemoryUsage: raw.MemoryStats.Usage,
		MemoryLimit: raw.MemoryStats.Limit,
		PIDs:        raw.PidsStats.Current,
	}

	if raw.MemoryStats.Limit > 0 {
		stats.MemoryPercent = float64(raw.MemoryStats.Usage) / float64(raw.MemoryStats.Limit) * 100
	}

	// CPU usage percentage
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	if sysDelta > 0 && raw.CPUStats.OnlineCPUs > 0 {
		stats.CPUUsagePercent = (cpuDelta / sysDelta) * float64(raw.CPUStats.OnlineCPUs) * 100
	}

	// Network totals across all interfaces
	for _, netStats := range raw.Networks {
		stats.NetworkRxBytes += netStats.RxBytes
		stats.NetworkTxBytes += netStats.TxBytes
	}

	// Block I/O
	for _, blkio := range raw.BlkioIOServiceBytes {
		switch blkio.Op {
		case "Read":
			stats.BlockReadBytes += blkio.Value
		case "Write":
			stats.BlockWriteBytes += blkio.Value
		}
	}

	return stats, nil
}

// DockerContainerLogs returns the last tail lines of container logs.
func DockerContainerLogs(containerID string, tail int) (string, error) {
	if !validContainerID(containerID) {
		return "", fmt.Errorf("invalid container ID")
	}
	if tail <= 0 {
		tail = 100
	}
	path := fmt.Sprintf("/containers/%s/logs?tail=%d&stdout=true&stderr=true&timestamps=true", containerID, tail)
	data, status, err := dockerRequest("GET", path, nil)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker logs returned %d: %s", status, string(data))
	}

	// Docker multiplexed stream: 8-byte header per frame.
	// Simplified: strip the header bytes and return raw text.
	// For most cases the text output is clean enough after stripping headers.
	output := stripDockerStreamHeaders(data)
	return capDockerOutput(output), nil
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

func capDockerOutput(value string) string {
	if len(value) > maxDockerOutputBytes {
		return value[:maxDockerOutputBytes]
	}
	return value
}

// stripDockerStreamHeaders removes the 8-byte Docker stream headers from log output.
// If the data doesn't look like a Docker multiplexed stream (header bytes 1-3 are non-zero
// or size exceeds remaining data), returns the raw data as-is.
func stripDockerStreamHeaders(data []byte) string {
	// Quick heuristic: Docker stream frames have bytes[1..3] == 0 and
	// a size in bytes[4..7] that doesn't exceed remaining data.
	if !looksLikeDockerStream(data) {
		return string(data)
	}

	var out strings.Builder
	for len(data) >= 8 {
		size := uint32(data[4])<<24 | uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
		data = data[8:]
		if int(size) > len(data) {
			size = uint32(len(data))
		}
		out.Write(data[:size])
		data = data[size:]
	}
	return out.String()
}

// looksLikeDockerStream checks if data starts with a Docker stream frame header.
func looksLikeDockerStream(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	// Stream type byte (data[0]) should be 0, 1, or 2.
	if data[0] > 2 {
		return false
	}
	// Bytes 1-3 must be zero padding.
	if data[1] != 0 || data[2] != 0 || data[3] != 0 {
		return false
	}
	// Size in bytes 4-7 should be reasonable (not exceeding remaining data by too much).
	size := uint32(data[4])<<24 | uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
	return size <= uint32(len(data)-8)
}

// GetDockerInfo returns Docker engine system information.
func GetDockerInfo() (*DockerInfo, error) {
	data, status, err := dockerRequest("GET", "/info", nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("docker info returned %d: %s", status, string(data))
	}

	var raw dockerInfoResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal info: %w", err)
	}

	return &DockerInfo{
		ServerVersion:     raw.ServerVersion,
		ContainersTotal:   raw.Containers,
		ContainersRunning: raw.ContainersRunning,
		ContainersStopped: raw.ContainersStopped,
		ContainersPaused:  raw.ContainersPaused,
		ImagesCount:       raw.Images,
		Driver:            raw.Driver,
		DockerRootDir:     raw.DockerRootDir,
		KernelVersion:     raw.KernelVersion,
		OS:                raw.OperatingSystem,
		Architecture:      raw.Architecture,
		NCPU:              raw.NCPU,
		MemoryTotal:       raw.MemTotal,
	}, nil
}

// DockerContainerAction performs a lifecycle action on a container.
// Supported actions: start, stop, restart, kill.
func DockerContainerAction(containerID, action string) error {
	if !validContainerID(containerID) {
		return fmt.Errorf("invalid container ID")
	}
	switch action {
	case "start", "stop", "restart", "kill":
	default:
		return fmt.Errorf("unsupported docker action: %s", action)
	}
	_, status, err := dockerRequest("POST", "/containers/"+containerID+"/"+action, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("docker %s returned status %d", action, status)
	}
	return nil
}

// DockerContainerRemove force-removes a container.
func DockerContainerRemove(containerID string, force bool) error {
	if !validContainerID(containerID) {
		return fmt.Errorf("invalid container ID")
	}
	path := "/containers/" + containerID
	if force {
		path += "?force=true"
	}
	_, status, err := dockerRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("docker remove returned status %d", status)
	}
	return nil
}

// DockerSystemPrune removes unused Docker data (stopped containers, dangling images, unused networks).
func DockerSystemPrune() (string, error) {
	body := map[string]bool{"v": true}
	data, status, err := dockerRequest("POST", "/system/prune", body)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker system prune returned %d: %s", status, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return string(data), nil
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// DockerImagePrune removes dangling images.
func DockerImagePrune() (string, error) {
	data, status, err := dockerRequest("POST", "/images/prune", nil)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker image prune returned %d: %s", status, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return string(data), nil
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// DockerContainerPrune removes stopped containers.
func DockerContainerPrune() (string, error) {
	data, status, err := dockerRequest("POST", "/containers/prune", nil)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker container prune returned %d: %s", status, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return string(data), nil
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// DockerNetworkPrune removes unused networks.
func DockerNetworkPrune() (string, error) {
	data, status, err := dockerRequest("POST", "/networks/prune", nil)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker network prune returned %d: %s", status, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return string(data), nil
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// DockerExecCreate creates an exec instance in a running container.
func DockerExecCreate(containerID string, cmd []string) (string, error) {
	if !validContainerID(containerID) {
		return "", fmt.Errorf("invalid container ID")
	}
	if len(cmd) == 0 || len(cmd) > 32 {
		return "", fmt.Errorf("exec command must contain 1 to 32 arguments")
	}
	for _, arg := range cmd {
		if len(arg) == 0 || len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return "", fmt.Errorf("invalid exec argument")
		}
	}
	body := map[string]interface{}{
		"Cmd":          cmd,
		"AttachStdout": true,
		"AttachStderr": true,
	}
	data, status, err := dockerRequest("POST", "/containers/"+containerID+"/exec", body)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker exec create returned %d: %s", status, string(data))
	}
	var resp dockerExecCreateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("unmarshal exec response: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("empty exec ID returned")
	}
	return resp.ID, nil
}

// DockerExecStart runs a previously created exec instance and returns its output.
func DockerExecStart(execID string) (string, error) {
	if !validContainerID(execID) {
		return "", fmt.Errorf("invalid exec ID")
	}
	body := map[string]interface{}{
		"Detach": false,
		"Tty":    false,
	}
	data, status, err := dockerRequestWithTimeout("POST", "/exec/"+execID+"/start", body, dockerRequestTimeout)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("docker exec start returned %d: %s", status, string(data))
	}
	return capDockerOutput(stripDockerStreamHeaders(data)), nil
}

var composeRoots = []string{"/home", "/opt", "/srv", "/var/lib/lem"}

func validComposePath(path string) bool {
	if !filepath.IsAbs(path) || strings.Contains(path, "..") {
		return false
	}
	clean := filepath.Clean(path)
	for _, root := range composeRoots {
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func pathUnderAnyRoot(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr != nil {
			return false
		}
		resolved = filepath.Join(parent, filepath.Base(path))
	}
	for _, root := range composeRoots {
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(resolvedRoot, resolved)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validateComposePath(path string, forWrite bool) error {
	if !validComposePath(path) {
		return fmt.Errorf("compose path is outside approved roots")
	}
	clean := filepath.Clean(path)
	resolved := clean
	if forWrite {
		parent := filepath.Dir(clean)
		info, err := os.Stat(parent)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("compose parent directory is unavailable")
		}
		resolved, err = filepath.EvalSymlinks(parent)
		if err != nil {
			return fmt.Errorf("resolve compose parent: %w", err)
		}
		resolved = filepath.Join(resolved, filepath.Base(clean))
		// The final component must not be an existing symlink or directory:
		// writing through it would escape the approved roots.
		if info, err := os.Lstat(clean); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("compose target is a symlink")
			}
			if info.IsDir() {
				return fmt.Errorf("compose target is a directory")
			}
		}
	} else {
		var err error
		resolved, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return fmt.Errorf("resolve compose path: %w", err)
		}
	}
	if !pathUnderAnyRoot(resolved) {
		return fmt.Errorf("compose path is outside approved roots")
	}
	return nil
}

// DockerComposeFiles scans only operator-approved compose roots.
func DockerComposeFiles() ([]DockerComposeFile, error) {
	searchPaths := append([]string(nil), composeRoots...)
	composeNames := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	var found []DockerComposeFile
	seen := map[string]bool{}

	for _, root := range searchPaths {
		for _, name := range composeNames {
			matches, err := filepath.Glob(filepath.Join(root, "**", name))
			if err != nil {
				continue
			}
			for _, match := range matches {
				if err := validateComposePath(match, false); err != nil {
					continue
				}
				if seen[match] {
					continue
				}
				seen[match] = true

				// Skip paths inside common excluded directories.
				if shouldSkipComposePath(match) {
					continue
				}

				info, err := os.Stat(match)
				if err != nil {
					continue
				}
				// Skip files larger than 1MB.
				if info.Size() > 1<<20 {
					continue
				}

				content, err := os.ReadFile(match)
				if err != nil {
					continue
				}

				found = append(found, DockerComposeFile{
					Path:    match,
					Content: string(content),
					Size:    info.Size(),
				})
			}
		}
	}
	return found, nil
}

// DockerComposeWrite writes content to a compose file on the host.
func DockerComposeWrite(path, content string) error {
	if path == "" {
		return fmt.Errorf("compose file path required")
	}
	if err := validateComposePath(path, true); err != nil {
		return err
	}
	// Validate path looks like a compose file.
	base := filepath.Base(path)
	validNames := map[string]bool{
		"docker-compose.yml": true, "docker-compose.yaml": true,
		"compose.yml": true, "compose.yaml": true,
	}
	if !validNames[base] {
		return fmt.Errorf("invalid compose filename: %s", base)
	}
	if len(content) > 1<<20 {
		return fmt.Errorf("compose file is too large")
	}
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	// Re-validate after creating the parent: MkdirAll may have followed a link,
	// and the write itself must never follow a symlink.
	if err := validateComposePath(path, true); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lem-compose-*")
	if err != nil {
		return fmt.Errorf("create temp compose file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set compose file permissions: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write compose file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync compose file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close compose file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace compose file: %w", err)
	}
	return nil
}

// DockerComposeUp runs docker-compose up -d for the given compose file.
func DockerComposeUp(composePath string) (string, error) {
	if composePath == "" {
		return "", fmt.Errorf("compose file path required")
	}
	if err := validateComposePath(composePath, false); err != nil {
		return "", err
	}
	if _, err := os.Stat(composePath); err != nil {
		return "", fmt.Errorf("compose file not found: %w", err)
	}
	dir := filepath.Dir(composePath)

	// Try docker compose (v2 plugin) first, fall back to docker-compose.
	if output, err := runDockerComposeCommand(dir, "docker", "compose", "up", "-d"); err == nil {
		return output, nil
	}
	return runDockerComposeCommand(dir, "docker-compose", "up", "-d")
}

// DockerComposeDown runs docker-compose down for the given compose file.
func DockerComposeDown(composePath string) (string, error) {
	if composePath == "" {
		return "", fmt.Errorf("compose file path required")
	}
	if err := validateComposePath(composePath, false); err != nil {
		return "", err
	}
	if _, err := os.Stat(composePath); err != nil {
		return "", fmt.Errorf("compose file not found: %w", err)
	}
	dir := filepath.Dir(composePath)

	if output, err := runDockerComposeCommand(dir, "docker", "compose", "down"); err == nil {
		return output, nil
	}
	return runDockerComposeCommand(dir, "docker-compose", "down")
}

// DockerComposePs runs docker-compose ps for the given compose file.
func DockerComposePs(composePath string) (string, error) {
	if composePath == "" {
		return "", fmt.Errorf("compose file path required")
	}
	if err := validateComposePath(composePath, false); err != nil {
		return "", err
	}
	if _, err := os.Stat(composePath); err != nil {
		return "", fmt.Errorf("compose file not found: %w", err)
	}
	dir := filepath.Dir(composePath)

	if output, err := runDockerComposeCommand(dir, "docker", "compose", "ps"); err == nil {
		return output, nil
	}
	return runDockerComposeCommand(dir, "docker-compose", "ps")
}

// DockerComposeLogs runs docker-compose logs --tail=100 for the given compose file.
func DockerComposeLogs(composePath string) (string, error) {
	if composePath == "" {
		return "", fmt.Errorf("compose file path required")
	}
	if err := validateComposePath(composePath, false); err != nil {
		return "", err
	}
	if _, err := os.Stat(composePath); err != nil {
		return "", fmt.Errorf("compose file not found: %w", err)
	}
	dir := filepath.Dir(composePath)

	if output, err := runDockerComposeCommand(dir, "docker", "compose", "logs", "--tail=100", "--no-color"); err == nil {
		return output, nil
	}
	return runDockerComposeCommand(dir, "docker-compose", "logs", "--tail=100", "--no-color")
}

// runDockerComposeCommand executes a docker compose command in the given directory.
func runDockerComposeCommand(dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out cappedBuffer
	out.limit = maxDockerOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	output = capDockerOutput(output)
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("docker compose command timed out")
	}
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", name, err, output)
	}
	return output, nil
}

// validContainerID validates a Docker container ID (hex chars, 1-128 chars).
func validContainerID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// shouldSkipComposePath returns true for paths inside excluded directories.
func shouldSkipComposePath(path string) bool {
	excluded := []string{".cache", ".local/share/Trash", "node_modules", ".git", "/proc", "/sys", "/dev"}
	for _, ex := range excluded {
		if strings.Contains(path, "/"+ex+"/") || strings.HasPrefix(path, ex+"/") {
			return true
		}
	}
	return false
}
