package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectAppsUsesConfiguredSources(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)

	writeFakeCommand(t, bin, "apt-mark", "#!/bin/sh\nprintf 'vim\\ncurl\\n'\n")
	writeFakeCommand(t, bin, "flatpak", "#!/bin/sh\nprintf 'org.mozilla.firefox\\t141.0\\n'\n")
	writeFakeCommand(t, bin, "pacman", "#!/bin/sh\ncase \"$1\" in\n-Qqe) printf 'vim 9.1\\nforeign-tool 1.0\\n' ;;\n-Qmq) printf 'foreign-tool 1.0\\n' ;;\nesac\n")
	writeFakeCommand(t, bin, "paru", "#!/bin/sh\nprintf 'aur-tool 2.3\\n'\n")

	applications := filepath.Join(home, "Applications")
	localBin := filepath.Join(home, ".local", "bin")
	for _, dir := range []string{applications, localBin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(applications, "Example.AppImage"): "app",
		filepath.Join(applications, "notes.txt"):        "ignore",
		filepath.Join(localBin, "Local Tool.AppImage"):  "app",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(applications, "Example.AppImage"), filepath.Join(applications, "Ignored.AppImage")); err != nil {
		t.Fatal(err)
	}

	apps, err := CollectApps([]string{"apt", "flatpak", "pacman", "aur", "appimage"})
	if err != nil {
		t.Fatalf("CollectApps: %v", err)
	}
	if len(apps) != 7 {
		t.Fatalf("CollectApps returned %d entries, want 7: %#v", len(apps), apps)
	}

	byKey := make(map[string]AppInfo, len(apps))
	for _, app := range apps {
		byKey[app.Source+"\x00"+app.Name] = app
	}
	if app := byKey["pacman\x00vim"]; app.Version != "9.1" {
		t.Fatalf("pacman app = %#v", app)
	}
	if _, ok := byKey["pacman\x00foreign-tool"]; ok {
		t.Fatal("foreign pacman package must not be classified as pacman")
	}
	if app := byKey["appimage\x00Local Tool"]; app.Path != filepath.Join(localBin, "Local Tool.AppImage") {
		t.Fatalf("AppImage path = %#v", app)
	}
	if _, ok := byKey["appimage\x00Ignored"]; ok {
		t.Fatal("AppImage symlink must be skipped")
	}
}

func TestCollectAppsRejectsPartialInventory(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "apt-mark", "#!/bin/sh\nexit 1\n")

	apps, err := CollectApps([]string{"apt"})
	if err == nil {
		t.Fatal("installed source failure must be reported")
	}
	if apps != nil {
		t.Fatalf("partial inventory must not be returned: %#v", apps)
	}
}

func writeFakeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}
