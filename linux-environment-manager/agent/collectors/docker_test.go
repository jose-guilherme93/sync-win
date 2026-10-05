package collectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidContainerID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"abc123def456", true},
		{"ABCDEF123456", true},
		{"a1b2c3", true},
		{"", false},
		{"abc123def456ghi789jkl012mno345pqr678stu901vwx234yz", false}, // >128 chars
		{"abc-123", false},
		{"abc 123", false},
		{"abc.def", false},
		{"abc123!@#", false},
	}
	for _, tt := range tests {
		got := validContainerID(tt.id)
		if got != tt.want {
			t.Errorf("validContainerID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestStripDockerStreamHeaders(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
		{
			name:  "no headers",
			input: []byte("hello world"),
			want:  "hello world",
		},
		{
			name:  "single frame",
			input: append([]byte{1, 0, 0, 0, 0, 0, 0, 5}, []byte("hello")...),
			want:  "hello",
		},
		{
			name: "multiple frames",
			input: func() []byte {
				frame1 := append([]byte{1, 0, 0, 0, 0, 0, 0, 3}, []byte("abc")...)
				frame2 := append([]byte{1, 0, 0, 0, 0, 0, 0, 2}, []byte("de")...)
				return append(frame1, frame2...)
			}(),
			want: "abcde",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripDockerStreamHeaders(tt.input)
			if got != tt.want {
				t.Errorf("stripDockerStreamHeaders() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockerContainerStruct(t *testing.T) {
	c := DockerContainer{
		ID:     "abc123",
		Names:  []string{"/my-container"},
		Image:  "nginx:latest",
		State:  "running",
		Status: "Up 3 hours",
		Ports: []DockerPort{
			{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		},
		Mounts: []DockerMount{
			{Type: "volume", Source: "my-vol", Destination: "/data", RW: true},
		},
		CreatedAt: 1234567890,
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DockerContainer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != c.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, c.ID)
	}
	if len(decoded.Names) != 1 || decoded.Names[0] != "/my-container" {
		t.Errorf("Names = %v, want [/my-container]", decoded.Names)
	}
	if decoded.State != "running" {
		t.Errorf("State = %q, want %q", decoded.State, "running")
	}
	if len(decoded.Ports) != 1 || decoded.Ports[0].PublicPort != 8080 {
		t.Errorf("Ports[0].PublicPort = %d, want 8080", decoded.Ports[0].PublicPort)
	}
	if len(decoded.Mounts) != 1 || decoded.Mounts[0].Destination != "/data" {
		t.Errorf("Mounts[0].Destination = %q, want %q", decoded.Mounts[0].Destination, "/data")
	}
}

func TestDockerStatsStruct(t *testing.T) {
	s := DockerStats{
		CPUUsagePercent: 42.5,
		MemoryUsage:     1073741824,
		MemoryLimit:     4294967296,
		MemoryPercent:   25.0,
		NetworkRxBytes:  1048576,
		NetworkTxBytes:  524288,
		BlockReadBytes:  2097152,
		BlockWriteBytes: 1048576,
		PIDs:            12,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DockerStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.CPUUsagePercent != 42.5 {
		t.Errorf("CPUUsagePercent = %f, want 42.5", decoded.CPUUsagePercent)
	}
	if decoded.MemoryUsage != 1073741824 {
		t.Errorf("MemoryUsage = %d, want 1073741824", decoded.MemoryUsage)
	}
	if decoded.PIDs != 12 {
		t.Errorf("PIDs = %d, want 12", decoded.PIDs)
	}
}

func TestDockerInfoStruct(t *testing.T) {
	info := DockerInfo{
		ServerVersion:     "24.0.7",
		ContainersTotal:   5,
		ContainersRunning: 3,
		ContainersStopped: 1,
		ContainersPaused:  1,
		ImagesCount:       12,
		Driver:            "overlay2",
		DockerRootDir:     "/var/lib/docker",
		KernelVersion:     "6.8.0",
		OS:                "linux",
		Architecture:      "amd64",
		NCPU:              8,
		MemoryTotal:       17179869184,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DockerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ServerVersion != "24.0.7" {
		t.Errorf("ServerVersion = %q, want %q", decoded.ServerVersion, "24.0.7")
	}
	if decoded.ContainersRunning != 3 {
		t.Errorf("ContainersRunning = %d, want 3", decoded.ContainersRunning)
	}
	if decoded.Driver != "overlay2" {
		t.Errorf("Driver = %q, want %q", decoded.Driver, "overlay2")
	}
}

func TestDockerComposeFileStruct(t *testing.T) {
	f := DockerComposeFile{
		Path:    "/home/user/project/docker-compose.yml",
		Content: "version: '3'\nservices:\n  web:\n    image: nginx",
		Size:    1024,
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DockerComposeFile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Path != f.Path {
		t.Errorf("Path = %q, want %q", decoded.Path, f.Path)
	}
	if decoded.Size != 1024 {
		t.Errorf("Size = %d, want 1024", decoded.Size)
	}
}

func TestShouldSkipComposePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/home/user/docker-compose.yml", false},
		{"/home/user/.cache/docker-compose.yml", true},
		{"/home/user/node_modules/docker-compose.yml", true},
		{"/home/user/.git/docker-compose.yml", true},
		{"/proc/1/docker-compose.yml", true},
		{"/sys/class/docker-compose.yml", true},
		{"/root/docker-compose.yml", false},
		{"/opt/app/docker-compose.yml", false},
	}
	for _, tt := range tests {
		got := shouldSkipComposePath(tt.path)
		if got != tt.want {
			t.Errorf("shouldSkipComposePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDockerContainerActionInvalidAction(t *testing.T) {
	err := DockerContainerAction("abc123", "invalid")
	if err == nil {
		t.Error("expected error for invalid action")
	}
}

func TestDockerContainerActionInvalidID(t *testing.T) {
	err := DockerContainerAction("not-a-valid-id!!!", "start")
	if err == nil {
		t.Error("expected error for invalid container ID")
	}
}

func TestDockerContainerRemoveInvalidID(t *testing.T) {
	err := DockerContainerRemove("not-valid!!!", true)
	if err == nil {
		t.Error("expected error for invalid container ID")
	}
}

func TestDockerExecCreateEmptyCmd(t *testing.T) {
	_, err := DockerExecCreate("abc123", nil)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestDockerExecCreateInvalidID(t *testing.T) {
	_, err := DockerExecCreate("not-valid!!!", []string{"ls"})
	if err == nil {
		t.Error("expected error for invalid container ID")
	}
}

func TestComposePathAllowlist(t *testing.T) {
	for _, path := range []string{"relative/compose.yml", "/etc/docker-compose.yml", "/home/user/../etc/compose.yml"} {
		if validComposePath(path) {
			t.Errorf("path %q should be rejected", path)
		}
	}
	if !validComposePath("/home/user/compose.yml") {
		t.Error("approved compose path should be accepted")
	}
}

func TestDockerOutputCap(t *testing.T) {
	if got := capDockerOutput(string(make([]byte, maxDockerOutputBytes+1))); len(got) != maxDockerOutputBytes {
		t.Fatalf("capped output length = %d", len(got))
	}
}

func TestDockerExecArgumentLimits(t *testing.T) {
	args := make([]string, 33)
	for i := range args {
		args[i] = "echo"
	}
	if _, err := DockerExecCreate("abc123", args); err == nil {
		t.Fatal("too many exec arguments should be rejected")
	}
	if _, err := DockerExecCreate("abc123", []string{"echo", "x\x00y"}); err == nil {
		t.Fatal("NUL exec argument should be rejected")
	}
}

func TestDockerExecStartEmptyID(t *testing.T) {
	_, err := DockerExecStart("", 0)
	if err == nil {
		t.Error("expected error for empty exec ID")
	}
}

func withComposeRoot(t *testing.T, root string) {
	t.Helper()
	orig := composeRoots
	composeRoots = []string{root}
	t.Cleanup(func() { composeRoots = orig })
}

func TestComposeWriteRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	withComposeRoot(t, dir)

	target := filepath.Join(dir, "docker-compose.yml")
	if err := os.Symlink(filepath.Join(outside, "docker-compose.yml"), target); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if err := DockerComposeWrite(target, "services: {}\n"); err == nil {
		t.Fatal("symlink compose target was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "docker-compose.yml")); err == nil {
		t.Fatal("write escaped the approved root through a symlink")
	}
}

func TestComposeWriteWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	withComposeRoot(t, dir)

	target := filepath.Join(dir, "docker-compose.yml")
	if err := DockerComposeWrite(target, "services:\n  web: {}\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "services:\n  web: {}\n" {
		t.Fatalf("written content = %q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lem-compose-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestComposeWriteRejectsNonComposeFilename(t *testing.T) {
	dir := t.TempDir()
	withComposeRoot(t, dir)
	if err := DockerComposeWrite(filepath.Join(dir, "notes.txt"), "x"); err == nil {
		t.Fatal("non-compose filename was accepted")
	}
}
