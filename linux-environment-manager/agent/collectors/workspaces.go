package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WorkspaceDiscoveryConfig defines the explicit project files collected from
// operator-configured project roots.
type WorkspaceDiscoveryConfig struct {
	Roots         []string
	Files         []string
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// DiscoverWorkspaceFiles returns only the configured relative files that
// exist below the configured project roots. It never scans the home directory.
func DiscoverWorkspaceFiles(home string, cfg WorkspaceDiscoveryConfig) ([]SaveCandidate, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("home is required")
	}
	files := make([]string, 0, len(cfg.Files))
	for _, file := range cfg.Files {
		file = strings.TrimSpace(file)
		if file == "" || strings.ContainsAny(file, "\\\x00\r\n") || filepath.IsAbs(filepath.FromSlash(file)) {
			return nil, fmt.Errorf("invalid workspace file path: %q", file)
		}
		clean := filepath.Clean(filepath.FromSlash(file))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("invalid workspace file path: %q", file)
		}
		files = append(files, filepath.ToSlash(clean))
	}

	candidates := make(map[string]SaveCandidate)
	var total int64
	for _, root := range cfg.Roots {
		root = strings.TrimSpace(root)
		if root == "" || !safeSavePath(home, root) || hasSymlinkComponent(home, root) {
			continue
		}
		info, err := os.Lstat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat workspace root %s: %w", root, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		for _, file := range files {
			path := filepath.Join(root, filepath.FromSlash(file))
			if !safeSavePath(home, path) || hasSymlinkComponent(home, path) {
				continue
			}
			fileInfo, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("stat workspace file %s: %w", path, err)
			}
			if !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 {
				continue
			}
			if cfg.MaxFileBytes > 0 && fileInfo.Size() > cfg.MaxFileBytes {
				continue
			}
			rel, err := filepath.Rel(home, path)
			if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			relativePath := filepath.ToSlash(rel)
			if _, exists := candidates[relativePath]; exists {
				continue
			}
			if cfg.MaxTotalBytes > 0 && total+fileInfo.Size() > cfg.MaxTotalBytes {
				continue
			}
			candidates[relativePath] = SaveCandidate{Path: path, RelativePath: relativePath, SizeBytes: fileInfo.Size()}
			total += fileInfo.Size()
		}
	}

	result := make([]SaveCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result, nil
}
