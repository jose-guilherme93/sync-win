package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"sync-win/agent/collectors"
	"sync-win/agent/internal/contract"
)

const maxSaveBatchItems = 256

type saveConfigResponse struct {
	ExtraDirs     []string `json:"extra_dirs"`
	WorkspaceDirs []string `json:"workspace_dirs"`
}

type restoreSaveFilePayload struct {
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
	Encoding     string `json:"encoding,omitempty"`
}

type restoreCommandPayload struct {
	SourceDeviceID string                   `json:"source_device_id"`
	PrefixID       string                   `json:"prefix_id"`
	GameName       string                   `json:"game_name"`
	Files          []restoreSaveFilePayload `json:"files"`
}

type preparedRestoreFile struct {
	path string
	data []byte
}

func syncSaves(serverURL, deviceID, deviceToken string, st *agentState) error {
	if st.LastSaveSyncHashes == nil {
		st.LastSaveSyncHashes = map[string]string{}
	}
	status, body, err := getJSON(serverURL+"/api/devices/"+deviceID+"/sync-config", deviceToken)
	if err != nil {
		return fmt.Errorf("read save sync config: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("save sync config returned %d", status)
	}
	var config saveConfigResponse
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("decode save sync config: %w", err)
	}

	roots := make([]string, 0, len(syncwinContract.Collection.Saves.Roots))
	for _, root := range syncwinContract.Collection.Saves.Roots {
		roots = append(roots, contract.ExpandPath(root))
	}
	extras := make([]string, 0, len(config.ExtraDirs))
	for _, dir := range config.ExtraDirs {
		extras = append(extras, contract.ExpandPath(dir))
	}
	candidates, err := collectors.DiscoverSaves(agentHome(), collectors.SaveDiscoveryConfig{
		Roots:              roots,
		ExtraDirs:          extras,
		IncludeExtensions:  syncwinContract.Collection.Saves.IncludeExtensions,
		ExcludedDirs:       syncwinContract.Collection.Saves.ExcludedDirs,
		ExcludedExtensions: syncwinContract.Collection.Saves.ExcludedExtensions,
		MaxFileBytes:       syncwinContract.SaveUploadFileBytes(),
		MaxTotalBytes:      syncwinContract.Collection.Saves.MaxTotalBytesPerCycle,
		MaxDepth:           syncwinContract.Collection.Saves.MaxDepth,
	})
	if err != nil {
		return fmt.Errorf("discover saves: %w", err)
	}

	payloads, hashes := scanSaveCandidates(candidates, st)
	if len(payloads) == 0 {
		pruneSaveHashes(st, hashes)
		st.LastSaveSync = time.Now().UTC()
		st.save()
		return nil
	}
	chunks, err := chunkSavePayloads(payloads, deviceToken)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		responseBody, err := postJSONWithResponse(serverURL+"/api/devices/"+deviceID+"/sync", deviceToken, map[string]any{
			"device_token": deviceToken,
			"preferences":  chunk,
		})
		if err != nil {
			return err
		}
		var response preferenceSyncResponse
		if err := json.Unmarshal(responseBody, &response); err != nil {
			return fmt.Errorf("decode save sync response: %w", err)
		}
		for _, saved := range response.Saved {
			if hash, ok := hashes[saved.RelativePath]; ok {
				st.LastSaveSyncHashes[saved.RelativePath] = hash
			}
		}
		st.save()
		log.Printf("save sync chunk completed candidates=%d saved=%d rejected=%d", len(chunk), len(response.Saved), len(response.Rejected))
	}
	pruneSaveHashes(st, hashes)
	st.LastSaveSync = time.Now().UTC()
	st.save()
	return nil
}

func scanSaveCandidates(candidates []collectors.SaveCandidate, st *agentState) ([]preferencePayload, map[string]string) {
	hashes := make(map[string]string, len(candidates))
	payloads := make([]preferencePayload, 0, len(candidates))
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate.Path)
		if err != nil || len(data) == 0 || int64(len(data)) > syncwinContract.SaveUploadFileBytes() {
			continue
		}
		hash := hashContent(string(data))
		hashes[candidate.RelativePath] = hash
		if st.LastSaveSyncHashes[candidate.RelativePath] == hash {
			continue
		}
		payload := preferencePayload{
			Category:     "saves",
			Filename:     filepath.Base(candidate.Path),
			RelativePath: candidate.RelativePath,
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			payload.Content = base64.StdEncoding.EncodeToString(data)
			payload.Encoding = "base64"
		} else {
			payload.Content = string(data)
		}
		payloads = append(payloads, payload)
	}
	return payloads, hashes
}

func chunkSavePayloads(payloads []preferencePayload, deviceToken string) ([][]preferencePayload, error) {
	limit := int(syncwinContract.ServerLimitsMirrored.MaxRequestBytes)
	if limit <= 0 {
		limit = 2 << 20
	}
	chunks := make([][]preferencePayload, 0)
	current := make([]preferencePayload, 0)
	for _, payload := range payloads {
		trial := append(append([]preferencePayload(nil), current...), payload)
		if len(trial) > maxSaveBatchItems || requestSize(deviceToken, trial) > limit {
			if len(current) == 0 {
				return nil, fmt.Errorf("save payload is too large")
			}
			chunks = append(chunks, current)
			current = []preferencePayload{payload}
			if requestSize(deviceToken, current) > limit {
				return nil, fmt.Errorf("save payload is too large")
			}
			continue
		}
		current = trial
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func requestSize(deviceToken string, payloads []preferencePayload) int {
	data, _ := json.Marshal(map[string]any{"device_token": deviceToken, "preferences": payloads})
	return len(data)
}

func pruneSaveHashes(st *agentState, keep map[string]string) {
	for path := range st.LastSaveSyncHashes {
		if _, ok := keep[path]; !ok {
			delete(st.LastSaveSyncHashes, path)
		}
	}
}

func restoreSaves(raw string, allowedPrefixes []string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("restore payload is empty")
	}
	var payload restoreCommandPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, fmt.Errorf("decode restore payload: %w", err)
	}
	if len(payload.Files) == 0 {
		return 0, fmt.Errorf("restore payload contains no files")
	}
	home := agentHome()
	if home == "" {
		return 0, fmt.Errorf("home directory is unavailable")
	}
	prepared := make([]preparedRestoreFile, 0, len(payload.Files))
	seen := make(map[string]bool, len(payload.Files))
	var total int64
	for _, file := range payload.Files {
		// Confinement is checked before writing: a restore may only target the
		// configured save roots. This stops a compromised server from
		// overwriting shell startup files, the agent's own policy/allowlist, or
		// SSH material even when allow_restore_saves is enabled.
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.RelativePath)))
		if !restorePathAllowed(rel, allowedPrefixes) {
			return 0, fmt.Errorf("restore path is outside the configured save roots: %s", file.RelativePath)
		}
		path, err := safeRestorePath(home, file.RelativePath)
		if err != nil {
			return 0, err
		}
		if seen[path] {
			return 0, fmt.Errorf("duplicate restore path: %s", file.RelativePath)
		}
		seen[path] = true
		data := []byte(file.Content)
		switch file.Encoding {
		case "":
			if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
				return 0, fmt.Errorf("invalid text restore file: %s", file.RelativePath)
			}
		case "base64":
			data, err = base64.StdEncoding.DecodeString(file.Content)
			if err != nil {
				return 0, fmt.Errorf("decode restore file %s: %w", file.RelativePath, err)
			}
		default:
			return 0, fmt.Errorf("unsupported restore encoding: %s", file.Encoding)
		}
		if len(data) == 0 || int64(len(data)) > syncwinContract.SaveUploadFileBytes() {
			return 0, fmt.Errorf("restore file exceeds limits: %s", file.RelativePath)
		}
		total += int64(len(data))
		if syncwinContract.Collection.Saves.MaxTotalBytesPerCycle > 0 && total > syncwinContract.Collection.Saves.MaxTotalBytesPerCycle {
			return 0, fmt.Errorf("restore payload exceeds total save limit")
		}
		prepared = append(prepared, preparedRestoreFile{path: path, data: data})
	}
	for _, file := range prepared {
		if err := writeRestoreFile(file.path, file.data); err != nil {
			return 0, err
		}
	}
	return len(prepared), nil
}

// fetchSaveExtraDirs loads the operator-configured extra save directories from
// the server so restore can allow them in addition to the contract roots.
func fetchSaveExtraDirs(serverURL, deviceID, deviceToken string) ([]string, error) {
	status, body, err := getJSON(serverURL+"/api/devices/"+deviceID+"/sync-config", deviceToken)
	if err != nil {
		return nil, fmt.Errorf("read save sync config: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("save sync config returned %d", status)
	}
	var config saveConfigResponse
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("decode save sync config: %w", err)
	}
	return config.ExtraDirs, nil
}

// allowedRestorePrefixes returns the home-relative prefixes a restore may
// target: every contract save root (up to its first glob metacharacter) plus
// the operator-configured extra directories.
func allowedRestorePrefixes(extraDirs []string, home string) []string {
	patterns := append(append([]string(nil), syncwinContract.Collection.Saves.Roots...), extraDirs...)
	prefixes := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if prefix := homeRelativePrefix(home, pattern); prefix != "" {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

// homeRelativePrefix expands a save-root pattern and returns its home-relative
// prefix up to the first glob metacharacter. Patterns outside the home are
// ignored ("").
func homeRelativePrefix(home, pattern string) string {
	expanded := filepath.Clean(contract.ExpandPath(pattern))
	rel, err := filepath.Rel(home, expanded)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	slash := filepath.ToSlash(rel)
	if idx := strings.IndexAny(slash, "*?["); idx >= 0 {
		slash = slash[:idx]
	}
	return strings.TrimRight(slash, "/")
}

// deniedRestorePrefixes are refused even if a save root would otherwise match,
// as defense in depth against path confusion.
var deniedRestorePrefixes = []string{
	".config/sync-win",
	".ssh",
	".config/autostart",
	".config/systemd",
	".local/state/sync-win",
	".local/bin",
	".bashrc",
	".bash_profile",
	".profile",
	".zshrc",
}

// restorePathAllowed reports whether a home-relative path may be restored.
func restorePathAllowed(rel string, prefixes []string) bool {
	for _, denied := range deniedRestorePrefixes {
		if rel == denied || strings.HasPrefix(rel, denied+"/") {
			return false
		}
	}
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

func safeRestorePath(home, relative string) (string, error) {
	if relative == "" || strings.ContainsAny(relative, "\\\x00\r\n") || filepath.IsAbs(filepath.FromSlash(relative)) {
		return "", fmt.Errorf("invalid restore path: %q", relative)
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == ".." {
			return "", fmt.Errorf("restore path contains traversal: %q", relative)
		}
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !safePreferencePath(home, filepath.Join(home, clean)) {
		return "", fmt.Errorf("restore path escapes home: %q", relative)
	}
	path := filepath.Join(home, clean)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("restore target is a symlink: %q", relative)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat restore target: %w", err)
	}
	return path, nil
}

func writeRestoreFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := ensureNoSymlinkParents(path); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create restore directory: %w", err)
	}
	if err := ensureNoSymlinkParents(path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sync-win-restore-*")
	if err != nil {
		return fmt.Errorf("create restore temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set restore permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write restore file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close restore file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace restore file: %w", err)
	}
	return nil
}

func ensureNoSymlinkParents(path string) error {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	current := string(filepath.Separator)
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore path contains symlink: %s", current)
		}
	}
	return nil
}
