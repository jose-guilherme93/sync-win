package collectors

import (
	"strings"
	"testing"
)

func TestParseJournalLine(t *testing.T) {
	// journalctl short-iso: timestamp, hostname, service[pid], message.
	line := "2026-10-07T13:03:15-0300 laptop kernel: EXT4-fs error on nvme0n1p2"
	got := parseJournalLine(line)
	if got.Timestamp != "2026-10-07T13:03:15-0300" {
		t.Errorf("timestamp = %q", got.Timestamp)
	}
	if got.Source != "kernel" {
		t.Errorf("source = %q, want kernel (the pid suffix must be stripped)", got.Source)
	}
	if got.Message != "EXT4-fs error on nvme0n1p2" {
		t.Errorf("message = %q", got.Message)
	}
	if got.Level != "error" {
		t.Errorf("level = %q, want error", got.Level)
	}
}

func TestParseJournalLineLevels(t *testing.T) {
	for _, tc := range []struct {
		message string
		level   string
	}{
		{"Failed to start unit", "error"},
		{"kernel: CRITICAL failure", "error"},
		// Severity comes from keywords in the message, since journalctl is asked
		// for the short-iso format and that carries no explicit priority.
		{"NetworkManager: state changed", "info"},
		{"NetworkManager: WARNING carrier lost", "warn"},
		{"Started unit", "info"},
	} {
		got := parseJournalLine("2026-10-07T13:03:15-0300 laptop svc: " + tc.message)
		if got.Level != tc.level {
			t.Errorf("parseJournalLine(%q).Level = %q, want %q", tc.message, got.Level, tc.level)
		}
	}
}

func TestCollectDeviceLogsReturnsErrorInsteadOfSilentNil(t *testing.T) {
	// A journal the agent cannot read must be distinguishable from a journal
	// with nothing in it. Returning (nil, nil) is what made an empty dashboard
	// impossible to tell apart from a broken one.
	logs, err := CollectDeviceLogs()
	if err != nil {
		if logs != nil {
			t.Errorf("lines = %d alongside error %v, want none", len(logs), err)
		}
		if !strings.Contains(err.Error(), "journalctl") {
			t.Errorf("error %q should name the failing command", err)
		}
		return
	}
	// No journal in this environment: success with nothing to report is the
	// only acceptable outcome.
	if logs == nil {
		t.Error("no error and no lines: an empty journal must still be an empty slice")
	}
}

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
