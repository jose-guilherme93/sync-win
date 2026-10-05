package collectors

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AppInfo is one entry in the installed application inventory.
type AppInfo struct {
	Source  string `json:"source"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

// CollectApps gathers the explicitly supported application sources. A source
// that is installed but fails is returned as an error so a partial inventory
// never replaces the last complete inventory on the server.
func CollectApps(sources []string) ([]AppInfo, error) {
	apps := make([]AppInfo, 0)
	var errs []error
	for _, source := range sources {
		var collected []AppInfo
		var err error
		switch source {
		case "apt":
			collected, err = collectAPTApps()
		case "flatpak":
			collected, err = collectFlatpakApps()
		case "pacman":
			collected, err = collectPacmanApps()
		case "aur":
			collected, err = collectAURApps()
		case "appimage":
			collected, err = collectAppImages()
		default:
			err = fmt.Errorf("unsupported app source %q", source)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", source, err))
			continue
		}
		apps = append(apps, collected...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Source != apps[j].Source {
			return apps[i].Source < apps[j].Source
		}
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		if apps[i].Version != apps[j].Version {
			return apps[i].Version < apps[j].Version
		}
		return apps[i].Path < apps[j].Path
	})
	unique := apps[:0]
	for _, app := range apps {
		if len(unique) == 0 || app != unique[len(unique)-1] {
			unique = append(unique, app)
		}
	}
	return unique, nil
}

func collectAPTApps() ([]AppInfo, error) {
	output, available, err := commandOutput("apt-mark", "showmanual")
	if err != nil || !available {
		return nil, err
	}
	var apps []AppInfo
	for _, name := range strings.Fields(string(output)) {
		apps = append(apps, AppInfo{Source: "apt", Name: name})
	}
	return apps, nil
}

func collectFlatpakApps() ([]AppInfo, error) {
	output, available, err := commandOutput("flatpak", "--user", "list", "--app", "--columns=application,version")
	if err != nil || !available {
		return nil, err
	}
	return parseNameVersionApps(output, "flatpak"), nil
}

func collectPacmanApps() ([]AppInfo, error) {
	explicit, available, err := commandOutput("pacman", "-Qqe")
	if err != nil || !available {
		return nil, err
	}
	foreignOutput, _, err := commandOutput("pacman", "-Qmq")
	if err != nil {
		return nil, err
	}
	foreign := make(map[string]bool)
	for _, app := range parseNameVersionApps(foreignOutput, "pacman") {
		foreign[app.Name] = true
	}
	apps := parseNameVersionApps(explicit, "pacman")
	filtered := apps[:0]
	for _, app := range apps {
		if !foreign[app.Name] {
			filtered = append(filtered, app)
		}
	}
	return filtered, nil
}

func collectAURApps() ([]AppInfo, error) {
	var apps []AppInfo
	foundHelper := false
	for _, helper := range []string{"paru", "yay"} {
		output, available, err := commandOutput(helper, "-Qmq")
		if err != nil {
			return nil, err
		}
		if available {
			foundHelper = true
			apps = append(apps, parseNameVersionApps(output, "aur")...)
		}
	}
	if !foundHelper {
		return nil, nil
	}
	return apps, nil
}

func collectAppImages() ([]AppInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, err
	}
	var apps []AppInfo
	for _, dir := range []string{filepath.Join(home, "Applications"), filepath.Join(home, ".local", "bin")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, entry := range entries {
			ext := filepath.Ext(entry.Name())
			if !strings.EqualFold(ext, ".appimage") || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			apps = append(apps, AppInfo{
				Source: "appimage",
				Name:   strings.TrimSuffix(entry.Name(), ext),
				Path:   filepath.Join(dir, entry.Name()),
			})
		}
	}
	return apps, nil
}

func commandOutput(name string, args ...string) ([]byte, bool, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	output, err := exec.Command(path, args...).Output()
	if err != nil {
		return nil, true, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, true, nil
}

func parseNameVersionApps(output []byte, source string) []AppInfo {
	var apps []AppInfo
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		app := AppInfo{Source: source, Name: fields[0]}
		if len(fields) > 1 {
			app.Version = fields[1]
		}
		apps = append(apps, app)
	}
	return apps
}
