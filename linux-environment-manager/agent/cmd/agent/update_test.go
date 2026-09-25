package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateAgentVerifiesAndReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "lem-agent")
	newBinary := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.7.0; fi\n")
	if err := os.WriteFile(current, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 0.6.1; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBinary)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/version":
			_, _ = w.Write([]byte(`{"version":"0.7.0"}`))
		case "/api/agent/download":
			_, _ = w.Write(newBinary)
		case "/api/agent/checksums":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  lem-agent\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	mockBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(mockBin, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := updateAgentAt(current, server.URL, "lem-agent.service", false); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(current)
	if err != nil || string(updated) != string(newBinary) {
		t.Fatalf("binary was not replaced: %q err=%v", updated, err)
	}
}

func TestNewerAgentVersion(t *testing.T) {
	cases := []struct {
		remote, current string
		want            bool
	}{
		{"0.6.2", "0.6.1", true},
		{"0.7.0", "0.6.9", true},
		{"0.6.1", "0.6.1", false},
		{"0.6.0", "0.6.1", false},
		{"v1.2.3", "1.2.2", true},
		{"0.6.2", "unknown", true},
	}
	for _, tc := range cases {
		got, err := newerAgentVersion(tc.remote, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("newerAgentVersion(%q, %q) = %v, %v; want %v", tc.remote, tc.current, got, err, tc.want)
		}
	}
}

func TestParseAgentVersionRejectsInvalid(t *testing.T) {
	for _, value := range []string{"", "1", "1.2.3.4", "1.x.3", "-1.0.0"} {
		if _, err := parseAgentVersion(value); err == nil {
			t.Errorf("parseAgentVersion(%q) accepted invalid value", value)
		}
	}
}
