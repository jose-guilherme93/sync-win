package collectors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type PendingUpdate struct {
	Source         string `json:"source"`
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version,omitempty"`
	NewVersion     string `json:"new_version"`
}

type UpdateInventory struct {
	Status    string          `json:"status"`
	CheckedAt string          `json:"checked_at"`
	Message   string          `json:"message,omitempty"`
	Updates   []PendingUpdate `json:"updates"`
}

type updateOutput struct {
	buffer   bytes.Buffer
	limit    int
	written  int
	exceeded bool
}

func (b *updateOutput) Write(p []byte) (int, error) {
	original := len(p)
	b.written += original
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buffer.Write(p[:remaining])
	}
	if b.written > b.limit {
		b.exceeded = true
	}
	return original, nil
}

func (b *updateOutput) Bytes() []byte  { return b.buffer.Bytes() }
func (b *updateOutput) String() string { return b.buffer.String() }

// CollectPendingUpdates only runs package-manager queries that read cached or
// remote metadata. Missing managers are reported as unsupported, not as an
// empty update list; a failed installed manager makes the whole snapshot an error.
func CollectPendingUpdates(sources []string, timeout time.Duration, maxOutputBytes int) UpdateInventory {
	result := UpdateInventory{
		Status:    "ready",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Updates:   []PendingUpdate{},
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 64 << 10
	}
	checked := false
	for _, source := range sources {
		var program string
		var args []string
		switch source {
		case "apt":
			program, args = "apt", []string{"list", "--upgradable"}
		case "flatpak":
			program, args = "flatpak", []string{"remote-ls", "--updates", "--columns=application,version"}
		case "pacman":
			program, args = "pacman", []string{"-Qu"}
		case "aur":
			for _, helper := range []string{"paru", "yay"} {
				if _, err := exec.LookPath(helper); err == nil {
					program, args = helper, []string{"-Qua"}
					break
				}
			}
		case "appimage":
			continue
		default:
			return failedUpdateInventory(result, fmt.Errorf("unsupported update source %q", source))
		}
		if program == "" {
			continue
		}
		output, available, err := readOnlyPackageQuery(program, args, timeout, maxOutputBytes)
		if err != nil {
			return failedUpdateInventory(result, err)
		}
		if !available {
			continue
		}
		checked = true
		result.Updates = append(result.Updates, parsePendingUpdates(source, output)...)
	}
	if !checked {
		result.Status = "unsupported"
		result.Message = "No supported package manager is installed on this device."
		return result
	}
	sort.Slice(result.Updates, func(i, j int) bool {
		if result.Updates[i].Source != result.Updates[j].Source {
			return result.Updates[i].Source < result.Updates[j].Source
		}
		return result.Updates[i].Name < result.Updates[j].Name
	})
	return result
}

func failedUpdateInventory(result UpdateInventory, err error) UpdateInventory {
	result.Status = "error"
	result.Message = err.Error()
	result.Updates = []PendingUpdate{}
	return result
}

func readOnlyPackageQuery(program string, args []string, timeout time.Duration, maxOutputBytes int) ([]byte, bool, error) {
	path, err := exec.LookPath(program)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr updateOutput
	stdout.limit = maxOutputBytes
	stderr.limit = maxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, true, fmt.Errorf("%s query timed out after %s", program, timeout)
		}
		return nil, true, fmt.Errorf("%s query failed: %w", program, err)
	}
	if stdout.exceeded || stderr.exceeded || stdout.written+stderr.written > maxOutputBytes {
		return nil, true, fmt.Errorf("%s query exceeded %d output bytes", program, maxOutputBytes)
	}
	return stdout.Bytes(), true, nil
}

func parsePendingUpdates(source string, output []byte) []PendingUpdate {
	var updates []PendingUpdate
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		switch source {
		case "apt":
			const marker = "[upgradable from: "
			markerAt := strings.Index(line, marker)
			if len(fields) < 2 || markerAt < 0 {
				continue
			}
			name := strings.SplitN(fields[0], "/", 2)[0]
			current := strings.TrimSpace(line[markerAt+len(marker):])
			current = strings.TrimSuffix(current, "]")
			updates = append(updates, PendingUpdate{Source: source, Name: name, CurrentVersion: current, NewVersion: fields[1]})
		case "pacman", "aur":
			if len(fields) >= 4 && fields[2] == "->" {
				updates = append(updates, PendingUpdate{Source: source, Name: fields[0], CurrentVersion: fields[1], NewVersion: fields[3]})
			}
		case "flatpak":
			if len(fields) >= 2 {
				updates = append(updates, PendingUpdate{Source: source, Name: fields[0], NewVersion: fields[1]})
			}
		}
	}
	return updates
}
