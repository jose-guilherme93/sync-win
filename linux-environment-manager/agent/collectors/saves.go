package collectors

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SaveCandidate is a save file discovered inside an explicitly selected root.
type SaveCandidate struct {
	Path         string
	RelativePath string
	SizeBytes    int64
}

// SaveDiscoveryConfig contains the collection limits from the embedded contract.
type SaveDiscoveryConfig struct {
	Roots              []string
	ExtraDirs          []string
	IncludeExtensions  []string
	ExcludedDirs       []string
	ExcludedExtensions []string
	MaxFileBytes       int64
	MaxTotalBytes      int64
	MaxDepth           int
}

// DiscoverSaves walks only the configured roots and returns deterministic,
// size-bounded candidates. It never follows symlinks.
func DiscoverSaves(home string, cfg SaveDiscoveryConfig) ([]SaveCandidate, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("home is required")
	}
	include := extensionSet(cfg.IncludeExtensions)
	excludedExtensions := extensionSet(cfg.ExcludedExtensions)
	excludedDirs := stringSetFold(cfg.ExcludedDirs)
	candidates := make(map[string]SaveCandidate)
	var total int64

	patterns := append(append([]string(nil), cfg.Roots...), cfg.ExtraDirs...)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid save root %q: %w", pattern, err)
		}
		for _, root := range matches {
			if !safeSavePath(home, root) {
				continue
			}
			info, err := os.Lstat(root)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("stat save root %s: %w", root, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if !info.IsDir() {
				if candidate, ok := saveCandidate(home, root, info, include, excludedExtensions); ok {
					if addSaveCandidate(candidates, &total, candidate, cfg) {
						continue
					}
				}
				continue
			}
			if excludedDirs[strings.ToLower(filepath.Base(root))] {
				continue
			}
			err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if path != root && entry.Type()&os.ModeSymlink != 0 {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.IsDir() {
					if path != root {
						if excludedDirs[strings.ToLower(entry.Name())] || pathDepth(root, path) > cfg.MaxDepth {
							return filepath.SkipDir
						}
					}
					return nil
				}
				if pathDepth(root, path) > cfg.MaxDepth {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				candidate, ok := saveCandidate(home, path, info, include, excludedExtensions)
				if !ok {
					return nil
				}
				addSaveCandidate(candidates, &total, candidate, cfg)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk save root %s: %w", root, err)
			}
		}
	}

	result := make([]SaveCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result, nil
}

func saveCandidate(home, path string, info fs.FileInfo, include, excluded map[string]bool) (SaveCandidate, bool) {
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return SaveCandidate{}, false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !include[ext] || excluded[ext] || !safeSavePath(home, path) || hasSymlinkComponent(home, path) {
		return SaveCandidate{}, false
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return SaveCandidate{}, false
	}
	return SaveCandidate{Path: path, RelativePath: filepath.ToSlash(rel), SizeBytes: info.Size()}, true
}

func addSaveCandidate(candidates map[string]SaveCandidate, total *int64, candidate SaveCandidate, cfg SaveDiscoveryConfig) bool {
	if _, exists := candidates[candidate.RelativePath]; exists {
		return false
	}
	if cfg.MaxFileBytes > 0 && candidate.SizeBytes > cfg.MaxFileBytes {
		return false
	}
	if cfg.MaxTotalBytes > 0 && *total+candidate.SizeBytes > cfg.MaxTotalBytes {
		return false
	}
	candidates[candidate.RelativePath] = candidate
	*total += candidate.SizeBytes
	return true
}

func hasSymlinkComponent(home, path string) bool {
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	current := home
	for _, part := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return !os.IsNotExist(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func safeSavePath(home, path string) bool {
	if home == "" || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(home, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(filepath.Clean(rel), string(filepath.Separator)))
}

func extensionSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func stringSetFold(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = true
		}
	}
	return set
}
