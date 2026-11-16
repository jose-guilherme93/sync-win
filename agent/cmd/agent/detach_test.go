package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A maintenance command started from a SyncWin remote terminal restarts the
// agent that carries that terminal. It must move itself behind PID 1 first, or
// the session dies with the work half done -- the case that left a device with
// no agent and no way in.
func TestDetachRequiredOnlyInsideRemoteSession(t *testing.T) {
	for _, tc := range []struct {
		name          string
		remoteSession string
		detached      string
		want          bool
	}{
		{"local run", "", "", false},
		{"inside a session", "ticket-1", "", true},
		{"already detached", "ticket-1", "1", false},
		{"detached flag with another value", "ticket-1", "0", true},
		{"blank session tag", "   ", "", false},
		{"detached with spaces", "ticket-1", " 1 ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detachRequired(tc.remoteSession, tc.detached); got != tc.want {
				t.Fatalf("detachRequired(%q, %q) = %v, want %v", tc.remoteSession, tc.detached, got, tc.want)
			}
		})
	}
}

// stubSystemdRun puts a fake systemd-run on PATH that records its argv, one
// element per line, and returns the path of the log.
func stubSystemdRun(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + logPath + "\n"
	if err := os.WriteFile(filepath.Join(dir, "systemd-run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return logPath
}

func TestReexecUpdateDetachedKeepsFlagsAsArguments(t *testing.T) {
	logPath := stubSystemdRun(t)

	// A value that would be a command if it ever reached a shell.
	hostile := "http://example.test; touch /tmp/syncwin-pwned"
	if err := reexecUpdateDetached([]string{"--server", hostile}); err != nil {
		t.Fatalf("reexecUpdateDetached: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("systemd-run was not called: %v", err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")

	if len(argv) < 5 {
		t.Fatalf("argv too short: %#v", argv)
	}
	if argv[0] != "--collect" {
		t.Errorf("first argument = %q, want --collect", argv[0])
	}
	unit := ""
	for _, arg := range argv {
		if strings.HasPrefix(arg, "--unit=syncwin-agent-update-") {
			unit = arg
		}
	}
	if unit == "" {
		t.Fatalf("no unit name in %#v; journalctl could not follow the run", argv)
	}
	if !containsArg(argv, "--setenv="+detachEnv+"=1") {
		t.Errorf("the detached run is not marked: %#v", argv)
	}
	// The binary and the subcommand must be plain arguments, not one shell line.
	updateIndex := indexOf(argv, "update")
	if updateIndex < 1 {
		t.Fatalf("no update subcommand in %#v", argv)
	}
	if !containsArg(argv, hostile) {
		t.Errorf("the flag value did not survive as one argument: %#v", argv)
	}
	if containsArg(argv, "/tmp/syncwin-pwned") {
		t.Errorf("a flag value became a separate argument: %#v", argv)
	}
}

func TestReexecUpdateDetachedFailsWithoutSystemdRun(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := reexecUpdateDetached(nil); err == nil {
		t.Fatal("a missing systemd-run must be reported, not silently ignored")
	}
}

func containsArg(argv []string, want string) bool {
	return indexOf(argv, want) >= 0
}

func indexOf(argv []string, want string) int {
	for i, arg := range argv {
		if arg == want {
			return i
		}
	}
	return -1
}
