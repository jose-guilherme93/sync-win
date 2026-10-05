package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWorkspaceFilesUsesExplicitPaths(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(root, ".vscode", "settings.json"): `{"editor.tabSize": 2}`,
		filepath.Join(root, ".vscode", "tasks.json"):    `{"version": "2.0.0"}`,
		filepath.Join(root, "notes.txt"):                "ignore",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, ".vscode", "settings.json"), filepath.Join(root, ".vscode", "launch.json")); err != nil {
		t.Fatal(err)
	}

	got, err := DiscoverWorkspaceFiles(home, WorkspaceDiscoveryConfig{
		Roots:         []string{root},
		Files:         []string{".vscode/settings.json", ".vscode/tasks.json", ".vscode/launch.json", "missing.json"},
		MaxFileBytes:  1024,
		MaxTotalBytes: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d workspace files, want 2: %#v", len(got), got)
	}
	if got[0].RelativePath != "projects/demo/.vscode/settings.json" || got[1].RelativePath != "projects/demo/.vscode/tasks.json" {
		t.Fatalf("unexpected workspace paths: %#v", got)
	}
}

func TestDiscoverWorkspaceFilesRejectsTraversal(t *testing.T) {
	if _, err := DiscoverWorkspaceFiles(t.TempDir(), WorkspaceDiscoveryConfig{Files: []string{"../secret.json"}}); err == nil {
		t.Fatal("workspace traversal path was accepted")
	}
}
