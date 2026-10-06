package collectors

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// fakePath puts the fake command directory ahead of the real PATH so a fixture
// script can still call ordinary tools (`cat`, `printf`) while shadowing the
// command under test. Tests that need a command to be genuinely absent keep the
// isolated empty PATH used by t.TempDir() on its own.
func fakePath(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCollectServicesFiltersStatesAndReadsEnabledFlag(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "systemctl", `#!/bin/sh
case "$1" in
list-units)
	printf 'sshd.service loaded active running OpenSSH server\n'
	printf 'nginx.service loaded failed failed nginx worker\n'
	printf 'old.service loaded inactive dead Retired unit\n'
	printf 'coredump.service loaded inactive dead Coredump\n'
	;;
list-unit-files)
	printf 'sshd.service enabled enabled\n'
	printf 'nginx.service enabled-runtime enabled-runtime\n'
	printf 'old.service disabled disabled\n'
	;;
esac
`)

	services, err := CollectServices(
		[]string{"running", "failed", "stopped"},
		[]string{"enabled", "enabled-runtime"},
		400, 0,
	)
	if err != nil {
		t.Fatalf("CollectServices: %v", err)
	}
	// stopped was requested, so inactive units are part of the result.
	if len(services) != 4 {
		t.Fatalf("got %d services, want 4: %#v", len(services), services)
	}
	byName := map[string]ServiceUnit{}
	for _, unit := range services {
		byName[unit.Name] = unit
	}

	// Failed units sort ahead of running ones regardless of name.
	if services[0].Name != "nginx.service" || services[1].Name != "sshd.service" {
		t.Fatalf("failed units must sort first: %#v", services)
	}
	if services[0].Status != "failed" {
		t.Fatalf("systemd ACTIVE=failed must map to status failed: %#v", services[0])
	}
	// systemd reports ACTIVE=active/SUB=running for a healthy service, so the
	// status has to come from the mapped value and not the raw column.
	sshd := byName["sshd.service"]
	if sshd.Status != "running" || sshd.ActiveState != "active" || sshd.SubState != "running" {
		t.Fatalf("sshd parsed wrong: %#v", sshd)
	}
	if sshd.Description != "OpenSSH server" {
		t.Fatalf("sshd description = %#v", sshd)
	}
	if !sshd.Enabled || sshd.UnitFileState != "enabled" {
		t.Fatalf("sshd enabled flag = %#v", sshd)
	}
	if !byName["nginx.service"].Enabled {
		t.Fatalf("enabled-runtime must count as enabled: %#v", byName["nginx.service"])
	}
	// ACTIVE=inactive must read as stopped, not as an unknown state.
	if old := byName["old.service"]; old.Status != "stopped" {
		t.Fatalf("inactive unit status = %#v", old)
	}
}

func TestCollectServicesCapsResult(t *testing.T) {
	bin := t.TempDir()
	fakePath(t, bin)
	writeFakeCommand(t, bin, "systemctl", `#!/bin/sh
case "$1" in
list-units)
	for i in 1 2 3 4 5; do printf 'svc%s.service loaded active running Unit %s\n' "$i" "$i"; done
	;;
esac
`)

	services, err := CollectServices([]string{"running"}, nil, 3, 0)
	if err != nil {
		t.Fatalf("CollectServices: %v", err)
	}
	if len(services) != 3 {
		t.Fatalf("max_units not applied: %d", len(services))
	}
	if services[0].Enabled {
		t.Fatal("a unit with no unit-file entry must not be reported as enabled")
	}
}

func TestCollectServicesWithoutSystemd(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	services, err := CollectServices([]string{"running"}, nil, 400, 0)
	if err != nil {
		t.Fatalf("a missing systemctl must not be an error: %v", err)
	}
	if services != nil {
		t.Fatalf("want nil without systemctl, got %#v", services)
	}
}

func TestCollectServicesReportsCommandFailure(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "systemctl", "#!/bin/sh\nexit 1\n")

	services, err := CollectServices([]string{"running"}, nil, 400, 0)
	if err == nil {
		t.Fatal("an installed but failing systemctl must be reported")
	}
	if services != nil {
		t.Fatalf("failed collection must return nothing: %#v", services)
	}
}

func TestCollectOpenPortsParsesSockets(t *testing.T) {
	bin := t.TempDir()
	fakePath(t, bin)
	writeFakeCommand(t, bin, "ss", `#!/bin/sh
cat <<'EOF'
tcp   LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*     users:(("sshd",pid=812,fd=3))
tcp   LISTEN 0      4096            [::]:22           [::]:*     users:(("sshd",pid=812,fd=4))
tcp   LISTEN 0      511        127.0.0.1:8080     0.0.0.0:*     users:(("node",pid=90,fd=20))
udp   UNCONN 0      0      127.0.0.53%lo:53      0.0.0.0:*     users:(("systemd-resolve",pid=750,fd=12))
tcp   ESTAB  0      0         10.0.0.2:52000     10.0.0.1:443
EOF
`)

	ports, err := CollectOpenPorts([]string{"LISTEN", "UNCONN"}, 200, 0)
	if err != nil {
		t.Fatalf("CollectOpenPorts: %v", err)
	}
	if len(ports) != 4 {
		t.Fatalf("got %d ports, want 4: %#v", len(ports), ports)
	}

	byKey := map[string]OpenPort{}
	for _, port := range ports {
		byKey[socketKey(port)] = port
	}

	if sshd := byKey["tcp|*|22"]; sshd.Process != "sshd" || sshd.PID != 812 {
		t.Fatalf("wildcard IPv4 listener parsed wrong: %#v", sshd)
	}
	if sshd := byKey["tcp|::|22"]; sshd.Process != "sshd" || sshd.PID != 812 {
		t.Fatalf("wildcard IPv6 listener parsed wrong: %#v", sshd)
	}
	if node := byKey["tcp|127.0.0.1|8080"]; node.Process != "node" || node.PID != 90 {
		t.Fatalf("loopback listener parsed wrong: %#v", node)
	}
	if resolver := byKey["udp|127.0.0.53|53"]; resolver.Process != "systemd-resolve" {
		t.Fatalf("zone-scoped address parsed wrong: %#v", resolver)
	}
	if _, ok := byKey["tcp|10.0.0.2|52000"]; ok {
		t.Fatal("ESTAB socket must be filtered out by the requested states")
	}
}

// socketKey builds a lookup key without the colon ambiguity that both a port
// number and an IPv6 host would otherwise introduce.
func socketKey(port OpenPort) string {
	host := port.Local
	if host == "" {
		host = "*"
	}
	return strings.Join([]string{port.Protocol, host, strconv.Itoa(port.Port)}, "|")
}

func TestCollectOpenPortsWithoutProcessColumn(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeFakeCommand(t, bin, "ss", "#!/bin/sh\nprintf 'tcp LISTEN 0 4096 0.0.0.0:631 0.0.0.0:*\\n'\n")

	ports, err := CollectOpenPorts([]string{"LISTEN"}, 200, 0)
	if err != nil {
		t.Fatalf("CollectOpenPorts: %v", err)
	}
	if len(ports) != 1 || ports[0].Port != 631 || ports[0].Process != "" {
		t.Fatalf("socket without a process must still be reported: %#v", ports)
	}
}

func TestCollectOpenPortsCapsResult(t *testing.T) {
	bin := t.TempDir()
	fakePath(t, bin)
	writeFakeCommand(t, bin, "ss", `#!/bin/sh
i=1
while [ "$i" -le 10 ]; do printf 'tcp LISTEN 0 4096 0.0.0.0:%s 0.0.0.0:*\n' "$i"; i=$((i + 1)); done
`)

	ports, err := CollectOpenPorts([]string{"LISTEN"}, 4, 0)
	if err != nil {
		t.Fatalf("CollectOpenPorts: %v", err)
	}
	if len(ports) != 4 {
		t.Fatalf("max_ports not applied: %d", len(ports))
	}
	if ports[0].Port != 1 {
		t.Fatalf("cap must keep the first rows after sorting: %#v", ports)
	}
}

func TestCollectOpenPortsWithoutSS(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	ports, err := CollectOpenPorts([]string{"LISTEN"}, 200, 0)
	if err != nil {
		t.Fatalf("a missing ss must not be an error: %v", err)
	}
	if ports != nil {
		t.Fatalf("want nil without ss, got %#v", ports)
	}
}

func TestParseSocketLineRejectsGarbage(t *testing.T) {
	wanted := map[string]bool{"LISTEN": true}
	for _, line := range []string{
		"",
		"tcp LISTEN",
		"tcp LISTEN 0 4096 0.0.0.0 0.0.0.0:*",
		"tcp LISTEN 0 4096 0.0.0.0:http 0.0.0.0:*",
	} {
		if _, ok := parseSocketLine(line, wanted); ok {
			t.Errorf("line %q must be rejected", line)
		}
	}
}
