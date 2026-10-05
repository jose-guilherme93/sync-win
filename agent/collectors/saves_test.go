package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSavesHonorsRootsFiltersAndDepth(t *testing.T) {
	home := t.TempDir()
	documents := filepath.Join(home, "roots", "one", "Documents")
	cache := filepath.Join(documents, "Game", "Cache")
	deep := filepath.Join(documents, "Game", "deep", "deeper")
	extras := filepath.Join(home, "extras")
	for _, dir := range []string{cache, deep, extras} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(documents, "Game", "slot.sav"):       "save",
		filepath.Join(documents, "Game", "notes.txt"):      "ignore",
		filepath.Join(cache, "cached.sav"):                 "ignore",
		filepath.Join(deep, "too-deep.sav"):                "ignore",
		filepath.Join(extras, "manual.dat"):                "data",
		filepath.Join(home, "ludusavi-config.yaml"):        "config",
		filepath.Join(home, "outside", "not-selected.sav"): "ignore",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(documents, "Game", "slot.sav"), filepath.Join(documents, "Game", "linked.sav")); err != nil {
		t.Fatal(err)
	}

	got, err := DiscoverSaves(home, SaveDiscoveryConfig{
		Roots:              []string{filepath.Join(home, "roots", "*", "Documents"), filepath.Join(home, "ludusavi-config.yaml")},
		ExtraDirs:          []string{extras},
		IncludeExtensions:  []string{".sav", ".dat", ".yaml"},
		ExcludedDirs:       []string{"Cache"},
		ExcludedExtensions: []string{".tmp"},
		MaxFileBytes:       10,
		MaxTotalBytes:      100,
		MaxDepth:           3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3: %#v", len(got), got)
	}
	paths := make(map[string]bool)
	for _, candidate := range got {
		paths[candidate.RelativePath] = true
	}
	for _, want := range []string{
		filepath.ToSlash(filepath.Join("roots", "one", "Documents", "Game", "slot.sav")),
		"extras/manual.dat",
		"ludusavi-config.yaml",
	} {
		if !paths[want] {
			t.Errorf("missing candidate %q: %#v", want, got)
		}
	}
	if paths[filepath.ToSlash(filepath.Join("roots", "one", "Documents", "Game", "linked.sav"))] {
		t.Error("symlink candidate was not skipped")
	}
}

func TestDiscoverSavesRejectsSymlinkIntermediateDirectory(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(home, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "slot.sav"), []byte("save"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverSaves(home, SaveDiscoveryConfig{
		Roots:             []string{link},
		IncludeExtensions: []string{".sav"},
		MaxFileBytes:      10,
		MaxDepth:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("symlink directory escaped home: %#v", got)
	}
}

func TestDiscoverSavesCapsTotalBytes(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "saves")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.sav", "two.sav"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := DiscoverSaves(home, SaveDiscoveryConfig{
		Roots:             []string{root},
		IncludeExtensions: []string{".sav"},
		MaxFileBytes:      10,
		MaxTotalBytes:     5,
		MaxDepth:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %#v", len(got), got)
	}
}
