package store

import (
	"path/filepath"
	"testing"
)

func newInventoryStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpdateSystemInventoryStoresBothSections(t *testing.T) {
	s := newInventoryStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	services := []ServiceUnit{
		{Name: "nginx.service", Status: "failed", ActiveState: "failed", Enabled: true},
		{Name: "sshd.service", Status: "running", ActiveState: "active"},
	}
	ports := []OpenPort{{Protocol: "tcp", Local: "", Port: 22, Process: "sshd", PID: 812}}
	if err := s.UpdateSystemInventory(device.ID, &services, &ports); err != nil {
		t.Fatalf("UpdateSystemInventory: %v", err)
	}

	gotServices, err := s.GetServices(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotServices) != 2 || gotServices[0].Name != "nginx.service" {
		t.Fatalf("services = %#v", gotServices)
	}
	if !gotServices[0].Enabled || gotServices[0].Status != "failed" {
		t.Fatalf("service fields lost: %#v", gotServices[0])
	}

	gotPorts, err := s.GetPorts(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotPorts) != 1 || gotPorts[0].Port != 22 || gotPorts[0].Process != "sshd" {
		t.Fatalf("ports = %#v", gotPorts)
	}
}

// A section the agent could not collect is sent as nil. Replacing the other
// section must leave the omitted one exactly as it was, or a transient systemctl
// failure would show up in the dashboard as a device with no services at all.
func TestUpdateSystemInventoryKeepsOmittedSection(t *testing.T) {
	s := newInventoryStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}

	services := []ServiceUnit{{Name: "sshd.service", Status: "running"}}
	ports := []OpenPort{{Protocol: "tcp", Port: 22}}
	if err := s.UpdateSystemInventory(device.ID, &services, &ports); err != nil {
		t.Fatal(err)
	}

	// Second upload reports only services, as it would if ss were unavailable.
	fresh := []ServiceUnit{{Name: "nginx.service", Status: "failed"}}
	if err := s.UpdateSystemInventory(device.ID, &fresh, nil); err != nil {
		t.Fatal(err)
	}

	gotServices, err := s.GetServices(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotServices) != 1 || gotServices[0].Name != "nginx.service" {
		t.Fatalf("services section must be replaced: %#v", gotServices)
	}
	gotPorts, err := s.GetPorts(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotPorts) != 1 || gotPorts[0].Port != 22 {
		t.Fatalf("omitted ports section must be preserved: %#v", gotPorts)
	}
}

func TestGetServicesBeforeFirstUpload(t *testing.T) {
	s := newInventoryStore(t)
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	services, err := s.GetServices(device.ID)
	if err != nil {
		t.Fatalf("a device that never reported must not error: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("want empty list, got %#v", services)
	}
	ports, err := s.GetPorts(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Fatalf("want empty list, got %#v", ports)
	}
}

func TestNormalizeServicesDropsUnnamedAndDuplicates(t *testing.T) {
	units := normalizeServices([]ServiceUnit{
		{Name: "  sshd.service  ", Status: " running "},
		{Name: "sshd.service"},
		{Name: "   "},
		{Name: "nginx.service"},
	})
	if len(units) != 2 {
		t.Fatalf("want 2 units, got %#v", units)
	}
	if units[0].Name != "sshd.service" || units[0].Status != "running" {
		t.Fatalf("fields must be trimmed: %#v", units[0])
	}
}

func TestNormalizeServicesEnforcesServerCap(t *testing.T) {
	units := make([]ServiceUnit, 0, maxSystemInventoryRows+50)
	for i := 0; i < maxSystemInventoryRows+50; i++ {
		units = append(units, ServiceUnit{Name: string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	if got := normalizeServices(units); len(got) != maxSystemInventoryRows {
		t.Fatalf("cap not applied: %d", len(got))
	}
}

func TestNormalizePortsRejectsUnusableEntries(t *testing.T) {
	ports := normalizePorts([]OpenPort{
		{Protocol: "TCP", Port: 22, Local: " 0.0.0.0 "},
		{Protocol: "", Port: 80},
		{Protocol: "tcp", Port: 0},
		{Protocol: "tcp", Port: 70000},
		{Protocol: "tcp", Port: 443},
	})
	if len(ports) != 2 {
		t.Fatalf("want 2 valid ports, got %#v", ports)
	}
	if ports[0].Protocol != "tcp" || ports[0].Local != "0.0.0.0" {
		t.Fatalf("port fields must be normalised: %#v", ports[0])
	}
}

// The inventory columns are added to an existing database by a migration, the
// same way an operator upgrading the image will hit them.
func TestSystemInventoryColumnsMigrateOnLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice("laptop", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the pre-feature schema by dropping the columns an upgraded
	// operator's database would not have yet.
	if _, err := s.db.Exec(`ALTER TABLE devices DROP COLUMN services_json`); err != nil {
		t.Skipf("sqlite build cannot drop a column: %v", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE devices DROP COLUMN ports_json`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopening must restore both columns without touching the existing device.
	again, err := NewStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("reopen after drop: %v", err)
	}
	defer again.Close()

	for _, column := range []string{"services_json", "ports_json"} {
		var count int
		again.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name=?", column).Scan(&count)
		if count != 1 {
			t.Fatalf("migration did not restore %s", column)
		}
	}
	services := []ServiceUnit{{Name: "sshd.service", Status: "running"}}
	if err := again.UpdateSystemInventory(device.ID, &services, nil); err != nil {
		t.Fatalf("write after migration: %v", err)
	}
	got, err := again.GetServices(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "sshd.service" {
		t.Fatalf("existing device lost its inventory: %#v", got)
	}
}
