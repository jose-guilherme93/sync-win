package collectors

import (
	"testing"
	"time"
)

func TestCollectPendingUpdatesParsesSupportedSources(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "apt", "#!/bin/sh\nprintf 'Listing...\\nvim/jammy-updates 9.2 amd64 [upgradable from: 9.1]\\n'\n")
	writeFakeCommand(t, bin, "flatpak", "#!/bin/sh\nprintf 'org.example.App\\t2.0\\n'\n")
	writeFakeCommand(t, bin, "pacman", "#!/bin/sh\nprintf 'kernel 6.1 -> 6.2\\n'\n")
	writeFakeCommand(t, bin, "paru", "#!/bin/sh\nprintf 'aur-tool 1.0 -> 1.1\\n'\n")

	got := CollectPendingUpdates([]string{"apt", "flatpak", "pacman", "aur", "appimage"}, time.Second, 4096)
	if got.Status != "ready" || len(got.Updates) != 4 {
		t.Fatalf("inventory = %+v", got)
	}
	want := []PendingUpdate{
		{Source: "apt", Name: "vim", CurrentVersion: "9.1", NewVersion: "9.2"},
		{Source: "aur", Name: "aur-tool", CurrentVersion: "1.0", NewVersion: "1.1"},
		{Source: "flatpak", Name: "org.example.App", NewVersion: "2.0"},
		{Source: "pacman", Name: "kernel", CurrentVersion: "6.1", NewVersion: "6.2"},
	}
	for i, update := range got.Updates {
		if update != want[i] {
			t.Errorf("update[%d] = %+v, want %+v", i, update, want[i])
		}
	}
}

func TestCollectPendingUpdatesDistinguishesUnsupportedAndFailures(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)

	unsupported := CollectPendingUpdates([]string{"apt", "flatpak", "pacman", "aur", "appimage"}, time.Second, 4096)
	if unsupported.Status != "unsupported" || len(unsupported.Updates) != 0 {
		t.Fatalf("unsupported inventory = %+v", unsupported)
	}

	writeFakeCommand(t, bin, "pacman", "#!/bin/sh\necho sync database unavailable >&2\nexit 1\n")
	failed := CollectPendingUpdates([]string{"pacman"}, time.Second, 4096)
	if failed.Status != "error" || failed.Message == "" || len(failed.Updates) != 0 {
		t.Fatalf("failed inventory = %+v", failed)
	}
}

func TestCollectPendingUpdatesBoundsQueryOutputAndTime(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "apt", "#!/bin/sh\nprintf 'output too long'\n")
	large := CollectPendingUpdates([]string{"apt"}, time.Second, 4)
	if large.Status != "error" {
		t.Fatalf("oversized output status = %q, want error", large.Status)
	}

	writeFakeCommand(t, bin, "apt", "#!/bin/sh\nsleep 2\n")
	timedOut := CollectPendingUpdates([]string{"apt"}, 20*time.Millisecond, 4096)
	if timedOut.Status != "error" {
		t.Fatalf("timed-out query status = %q, want error", timedOut.Status)
	}
}

func TestParsePendingUpdatesSkipsHeadersAndMalformedRows(t *testing.T) {
	updates := parsePendingUpdates("apt", []byte("Listing...\nnot a package row\nfoo/repo 2.0 amd64 [upgradable from: 1.0]\n"))
	if len(updates) != 1 || updates[0].Name != "foo" || updates[0].CurrentVersion != "1.0" {
		t.Fatalf("parsed updates = %+v", updates)
	}
}

func TestUpdateOutputRecordsOverflowAcrossWrites(t *testing.T) {
	var output updateOutput
	output.limit = 4
	_, _ = output.Write([]byte("abcd"))
	_, _ = output.Write([]byte("ef"))
	if !output.exceeded || output.written != 6 || output.String() != "abcd" {
		t.Fatalf("bounded output = %+v, text=%q", output, output.String())
	}
}
