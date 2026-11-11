package main

import (
	"strings"
	"testing"
)

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
