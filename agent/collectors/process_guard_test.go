package collectors

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

func guardForTest(t *testing.T) ProcessGuard {
	t.Helper()
	root := buildProcFixture(t)
	return ProcessGuard{
		ProcRoot:       root,
		AllowSignals:   true,
		AgentPID:       4242,
		AllowedSignals: []string{"TERM", "KILL", "STOP", "CONT"},
		ProtectedNames: []string{"systemd", "init", "dockerd", "containerd", "containerd-shim", "systemd-journald", "sshd", "sync-win-agent"},
		ProtectedUnits: []string{"ssh.service", "docker.service", "containerd.service", "systemd-journald.service", "sync-win-agent.service"},
		MinPID:         2,
		MaxPID:         4194304,
	}
}

func TestProcessSignalGuardTable(t *testing.T) {
	// firefox starts at btime 1000 + 500/100 = 1005.
	valid := ProcessSignalRequest{PID: 1000, Signal: "TERM", StartTimeUnix: 1005}

	cases := []struct {
		name    string
		mutate  func(*ProcessGuard, *ProcessSignalRequest)
		wantSig syscall.Signal
		wantErr string
	}{
		{
			name:    "policy denied",
			mutate:  func(g *ProcessGuard, _ *ProcessSignalRequest) { g.AllowSignals = false },
			wantErr: "disabled by local policy",
		},
		{
			name:    "signal outside allowlist",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.Signal = "HUP" },
			wantErr: "signal not allowed",
		},
		{
			name:    "pid out of range low",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 0 },
			wantErr: "pid out of range",
		},
		{
			name:    "pid out of range high",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 4194305 },
			wantErr: "pid out of range",
		},
		{
			name:    "pid 1 rejected",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 1; r.StartTimeUnix = 1001 },
			wantErr: "pid 1",
		},
		{
			name:    "process gone",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 9999 },
			wantErr: "process not found",
		},
		{
			name:    "pid reused",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.StartTimeUnix = 7777 },
			wantErr: "pid reused",
		},
		{
			name:    "own pid",
			mutate:  func(g *ProcessGuard, r *ProcessSignalRequest) { r.PID = 1000; g.AgentPID = 1000 },
			wantErr: "protected process",
		},
		{
			name:    "protected name systemd",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 1; r.StartTimeUnix = 1001 },
			wantErr: "pid 1",
		},
		{
			name:    "protected unit",
			mutate:  func(_ *ProcessGuard, r *ProcessSignalRequest) { r.PID = 50; r.StartTimeUnix = 1002 },
			wantErr: "protected",
		},
		{
			name:    "valid signal",
			mutate:  func(_ *ProcessGuard, _ *ProcessSignalRequest) {},
			wantSig: syscall.SIGTERM,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := guardForTest(t)
			req := valid
			tc.mutate(&g, &req)
			sig, err := g.CheckSignal(req)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if sig != tc.wantSig {
					t.Fatalf("signal = %v, want %v", sig, tc.wantSig)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if sig != 0 {
				t.Fatalf("a rejected signal must not return a syscall number, got %v", sig)
			}
		})
	}
}

func TestProcessSignalGuardProtectsZombiesAndNames(t *testing.T) {
	root := buildProcFixture(t)
	// A zombie with an allowed unit and a normal name.
	if err := writeFixtureFile(root, "2000/comm", "defunct\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2000/status", "Name:\tdefunct\nUid:\t1000\t1000\t1000\t1000\nVmRSS:\t0 kB\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2000/stat", "2000 (defunct) Z 1 2000 2000 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 600 0 0 18446744073709551615\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2000/cgroup", "0::/user.slice/user-1000.slice/session-2.scope\n"); err != nil {
		t.Fatal(err)
	}
	g := guardForTest(t)
	g.ProcRoot = root

	// Zombie: start time 1000 + 600/100 = 1006.
	if _, err := g.CheckSignal(ProcessSignalRequest{PID: 2000, Signal: "TERM", StartTimeUnix: 1006}); err == nil || !strings.Contains(err.Error(), "zombie") {
		t.Fatalf("zombie must be rejected, got %v", err)
	}

	// A protected name reached through a different pid.
	if err := writeFixtureFile(root, "2001/comm", "sshd\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2001/status", "Name:\tsshd\nUid:\t0\t0\t0\t0\nVmRSS:\t10 kB\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2001/stat", "2001 (sshd) S 1 2001 2001 0 -1 4194560 0 0 0 0 5 5 0 0 20 0 1 0 700 0 0 18446744073709551615\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureFile(root, "2001/cgroup", "0::/system.slice/user.slice\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CheckSignal(ProcessSignalRequest{PID: 2001, Signal: "TERM", StartTimeUnix: 1007}); err == nil || !strings.Contains(err.Error(), "protected process") {
		t.Fatalf("sshd must be rejected by name, got %v", err)
	}
}

func TestProcessSignalGuardKernelThread(t *testing.T) {
	g := guardForTest(t)
	// kworker starts at 1000 + 200/100 = 1002.
	if _, err := g.CheckSignal(ProcessSignalRequest{PID: 50, Signal: "TERM", StartTimeUnix: 1002}); err == nil {
		t.Fatal("kernel threads must be rejected")
	} else if !strings.Contains(err.Error(), "kernel thread") && !strings.Contains(err.Error(), "protected unit") {
		t.Fatalf("kernel thread error = %q", err.Error())
	}
}

func TestProcessSignalLimiter(t *testing.T) {
	limiter := NewProcessSignalLimiter(10, time.Minute)
	start := time.Unix(1000, 0)
	for i := 0; i < 10; i++ {
		if !limiter.Allow(start.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("signal %d should be allowed", i+1)
		}
	}
	if limiter.Allow(start.Add(10 * time.Second)) {
		t.Fatal("the 11th signal inside the window must be rate limited")
	}
	// A second window opens after a minute.
	if !limiter.Allow(start.Add(time.Minute + time.Second)) {
		t.Fatal("the limiter must reset after the window")
	}
}

func TestDecodeProcessSignalPayloadStrict(t *testing.T) {
	valid := []byte(`{"pid":1000,"signal":"TERM","start_time_unix":1005}`)
	req, err := DecodeProcessSignalPayload(valid)
	if err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if req.PID != 1000 || req.Signal != "TERM" || req.StartTimeUnix != 1005 {
		t.Fatalf("decoded = %+v", req)
	}

	for _, tc := range []struct {
		name    string
		payload string
		want    string
	}{
		{"unknown field", `{"pid":1000,"signal":"TERM","start_time_unix":1005,"extra":true}`, "unknown"},
		{"wrong type", `{"pid":"1000","signal":"TERM","start_time_unix":1005}`, ""},
		{"oversized", `{"pid":1000,"signal":"TERM","start_time_unix":1005,"pad":"` + strings.Repeat("x", 5000) + `"}`, "too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeProcessSignalPayload([]byte(tc.payload))
			if err == nil {
				t.Fatalf("payload must be rejected: %s", tc.payload)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateNiceAndIonice(t *testing.T) {
	for _, n := range []int{-20, -1, 0, 19} {
		if err := ValidateNicePriority(n); err != nil {
			t.Errorf("nice %d must be valid: %v", n, err)
		}
	}
	for _, n := range []int{-100, -21, 20, 1000} {
		if err := ValidateNicePriority(n); err == nil {
			t.Errorf("nice %d must be rejected", n)
		}
	}
	for _, tc := range []struct {
		class, level int
		ok           bool
	}{
		{1, 0, true}, {2, 4, true}, {3, 7, true},
		{0, 0, false}, {4, 0, false}, {2, 8, false}, {2, -1, false},
	} {
		err := ValidateIonice(tc.class, tc.level)
		if tc.ok && err != nil {
			t.Errorf("ionice %d/%d must be valid: %v", tc.class, tc.level, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ionice %d/%d must be rejected", tc.class, tc.level)
		}
	}
}

func writeFixtureFile(root, rel, content string) error {
	return writeFileForTest(root, rel, content)
}
