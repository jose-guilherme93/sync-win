package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The dispatch is the path that actually runs on a device; testing
// executeProcessCommand alone once let it ship without ever being called.
func TestExecuteCommandRoutesProcessCommands(t *testing.T) {
	t.Setenv("SYNCWIN_POLICY_PATH", filepath.Join(t.TempDir(), "missing-policy.json"))
	for _, typ := range []string{"process_signal", "process_renice", "process_ionice"} {
		result := executeCommand(agentCommand{Type: typ, Payload: `{}`}, "", "", "")
		if strings.Contains(result.Message, "unsupported command type") {
			t.Fatalf("%s fell through to the default branch: %+v", typ, result)
		}
		if result.Status != "failed" {
			t.Fatalf("%s = %+v, want a local-policy denial with signals off", typ, result)
		}
	}
}

func TestExecuteProcessCommandPolicyFailsClosed(t *testing.T) {
	policy := localPolicy{AllowProcessSignals: false}
	result := executeProcessCommand("process_signal", `{"pid":1000,"signal":"TERM","start_time_unix":1005}`, policy)
	if result.Status != "failed" || !strings.Contains(result.Message, "disabled by local policy") {
		t.Fatalf("policy off = %+v, want a local-policy denial", result)
	}
}

func TestExecuteProcessCommandRejectsUnknownType(t *testing.T) {
	result := executeProcessCommand("process_torture", `{}`, localPolicy{AllowProcessSignals: true})
	if result.Status != "failed" || !strings.Contains(result.Message, "unsupported") {
		t.Fatalf("unknown type = %+v", result)
	}
}

func TestExecuteProcessCommandRejectsBadPayload(t *testing.T) {
	policy := localPolicy{AllowProcessSignals: true}
	for _, payload := range []string{
		`{"pid":1000,"signal":"TERM","start_time_unix":1005,"unknown":1}`,
		`{"pid":"nope","signal":"TERM","start_time_unix":1005}`,
		`{"pid":1000,"signal":"TERM","start_time_unix":1005,"pad":"` + strings.Repeat("x", 5000) + `"}`,
	} {
		result := executeProcessCommand("process_signal", payload, policy)
		if result.Status != "failed" {
			t.Fatalf("payload %q must fail, got %+v", payload, result)
		}
	}
}
