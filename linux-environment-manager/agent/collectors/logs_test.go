package collectors

import (
	"strings"
	"testing"
)

func TestRedactLogMessage(t *testing.T) {
	secrets := []struct {
		input  string
		leaked string
	}{
		{"db password=hunter2 connected", "hunter2"},
		{"Authorization: Bearer abc.def.ghi", "abc.def.ghi"},
		{"token=ghp_abcdefghijklmnopqrstuvwxyz012345", "ghp_"},
		{"key -----BEGIN RSA PRIVATE KEY-----", "PRIVATE KEY"},
	}
	for _, tc := range secrets {
		got := redactLogMessage(tc.input)
		if !strings.Contains(got, "[REDACTED]") {
			t.Errorf("redactLogMessage(%q) = %q, want [REDACTED]", tc.input, got)
		}
		if strings.Contains(got, tc.leaked) {
			t.Errorf("redactLogMessage(%q) = %q leaked %q", tc.input, got, tc.leaked)
		}
	}
	if got := redactLogMessage("service started normally"); got != "service started normally" {
		t.Errorf("benign message changed: %q", got)
	}
	long := strings.Repeat("x", maxLogMessageBytes+100)
	if got := redactLogMessage(long); len(got) != maxLogMessageBytes {
		t.Errorf("length = %d, want %d", len(got), maxLogMessageBytes)
	}
}
