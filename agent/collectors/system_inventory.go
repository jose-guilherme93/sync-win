package collectors

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ServiceUnit is one systemd service unit. Only the state the dashboard renders
// is reported; nothing here is user data and nothing is written to disk.
type ServiceUnit struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	LoadState     string `json:"load_state"`
	ActiveState   string `json:"active_state"`
	SubState      string `json:"sub_state,omitempty"`
	UnitFileState string `json:"unit_file_state,omitempty"`
	Description   string `json:"description,omitempty"`
	Enabled       bool   `json:"enabled"`
}

// unitStatus maps systemd's ACTIVE column onto the three buckets the contract
// and the dashboard speak: running, failed and stopped. systemd reports values
// like `active`, `reloading`, `activating` and `inactive`, which mean nothing to
// a reader of the dashboard. The raw column is still reported alongside it.
func unitStatus(activeState string) string {
	switch activeState {
	case "active":
		return "running"
	case "failed":
		return "failed"
	default:
		return "stopped"
	}
}

// OpenPort is one listening socket. The owning process is reported by name and
// pid only, never by command line, so no user data leaves the device.
type OpenPort struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local_address"`
	Port     int    `json:"port"`
	Process  string `json:"process,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// CollectServices enumerates systemd services and keeps only the units whose
// active state is in states. An absent systemctl is not an error: the agent also
// runs on systems without systemd and a missing tool must not be reported as a
// cycle failure every five minutes.
func CollectServices(states []string, enabledStates []string, max int, timeout time.Duration) ([]ServiceUnit, error) {
	if max <= 0 {
		max = 400
	}
	units, available, err := commandOutputWithTimeout(timeout, "systemctl",
		"list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, nil
	}
	// Unit file state lives in a separate listing. A failure here only costs the
	// enabled flag, so it downgrades to an empty map rather than failing the
	// whole collection.
	files, _, fileErr := commandOutputWithTimeout(timeout, "systemctl",
		"list-unit-files", "--type=service", "--no-legend", "--no-pager", "--plain")
	if fileErr != nil {
		files = nil
	}
	fileStates := parseUnitFiles(files)

	enabled := make(map[string]bool, len(fileStates))
	for name, state := range fileStates {
		enabled[name] = containsFold(enabledStates, state)
	}
	wanted := make(map[string]bool, len(states))
	for _, state := range states {
		wanted[strings.ToLower(state)] = true
	}

	var services []ServiceUnit
	for _, line := range strings.Split(string(units), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		unit := ServiceUnit{
			Name:          fields[0],
			Status:        unitStatus(fields[2]),
			LoadState:     fields[1],
			ActiveState:   fields[2],
			SubState:      fields[3],
			Description:   strings.Join(fields[4:], " "),
			UnitFileState: fileStates[fields[0]],
			Enabled:       enabled[fields[0]],
		}
		if !wanted[strings.ToLower(unit.Status)] {
			continue
		}
		services = append(services, unit)
	}

	// Failed units first: they are the reason the dashboard has this screen, and
	// a name-sorted list would bury them among hundreds of stopped ones.
	sort.Slice(services, func(i, j int) bool {
		if services[i].Status != services[j].Status {
			return statusRank(services[i].Status) < statusRank(services[j].Status)
		}
		return services[i].Name < services[j].Name
	})
	if len(services) > max {
		services = services[:max]
	}
	return services, nil
}

// CollectOpenPorts reads the listening socket table. Like CollectServices it
// treats a missing tool as "nothing to report" rather than a failure.
func CollectOpenPorts(states []string, max int, timeout time.Duration) ([]OpenPort, error) {
	if max <= 0 {
		max = 200
	}
	output, available, err := commandOutputWithTimeout(timeout, "ss", "-tulpnH")
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, nil
	}

	wanted := make(map[string]bool, len(states))
	for _, state := range states {
		wanted[strings.ToUpper(state)] = true
	}

	var ports []OpenPort
	for _, line := range strings.Split(string(output), "\n") {
		port, ok := parseSocketLine(line, wanted)
		if ok {
			ports = append(ports, port)
		}
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Protocol != ports[j].Protocol {
			return ports[i].Protocol < ports[j].Protocol
		}
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Local < ports[j].Local
	})
	if len(ports) > max {
		ports = ports[:max]
	}
	return ports, nil
}

// socketStateRe matches the ss state column (LISTEN, UNCONN, ESTAB, ...). Some
// builds omit it for datagram sockets, which would otherwise shift every field.
var socketStateRe = regexp.MustCompile(`^[A-Z][A-Z-]*$`)

// processRe matches ss's `users:(("sshd",pid=812,fd=3))` suffix.
var processRe = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// parseSocketLine reads one `ss -tulpnH` record. Columns are netid, state,
// recv-q, send-q, local and peer, optionally followed by the process suffix.
func parseSocketLine(line string, wanted map[string]bool) (OpenPort, bool) {
	// The process suffix is optional and always last. Split it off before the
	// fixed columns are read, but match it against the untouched line.
	columns := line
	process := ""
	if index := strings.Index(line, "users:"); index >= 0 {
		columns = line[:index]
		process = line[index:]
	}
	fields := strings.Fields(columns)
	if len(fields) < 5 {
		return OpenPort{}, false
	}

	protocol := strings.ToLower(fields[0])
	rest := fields[1:]
	state := ""
	if socketStateRe.MatchString(rest[0]) {
		state = rest[0]
		rest = rest[1:]
	}
	// What remains must be recv-q, send-q, local address and peer address.
	if len(rest) < 4 {
		return OpenPort{}, false
	}
	if state != "" && len(wanted) > 0 && !wanted[state] {
		return OpenPort{}, false
	}

	host, port, err := splitSocketAddress(rest[2])
	if err != nil {
		return OpenPort{}, false
	}
	entry := OpenPort{Protocol: protocol, Local: host, Port: port}
	if match := processRe.FindStringSubmatch(process); len(match) == 3 {
		entry.Process = match[1]
		if pid, err := strconv.Atoi(match[2]); err == nil {
			entry.PID = pid
		}
	}
	return entry, true
}

// splitSocketAddress turns `0.0.0.0:22` or `[::%eth0]:22` into a host and port.
// The wildcard and link-local addresses carry no routable host, so they are
// normalised to the empty string and `::` respectively.
func splitSocketAddress(address string) (string, int, error) {
	index := strings.LastIndex(address, ":")
	if index < 0 {
		return "", 0, errors.New("socket address has no port")
	}
	port, err := strconv.Atoi(address[index+1:])
	if err != nil {
		return "", 0, err
	}
	host := strings.Trim(address[:index], "[]")
	if zone := strings.Index(host, "%"); zone >= 0 {
		host = host[:zone]
	}
	switch host {
	case "0.0.0.0", "":
		host = ""
	case "*":
		host = ""
	}
	return host, port, nil
}

func parseUnitFiles(output []byte) map[string]string {
	units := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		units[fields[0]] = fields[1]
	}
	return units
}

// statusRank orders the buckets by how much they matter on the Services screen.
func statusRank(status string) int {
	switch status {
	case "failed":
		return 0
	case "running":
		return 1
	default:
		return 2
	}
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

// commandOutputWithTimeout runs one bounded, read-only inventory command. A
// missing binary is reported as unavailable rather than as an error so callers
// can degrade quietly on systems that do not ship the tool.
func commandOutputWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, bool, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).Output()
	if ctx.Err() != nil {
		return nil, true, fmt.Errorf("%s timed out after %s", name, timeout)
	}
	if err != nil {
		return nil, true, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, true, nil
}
