package store

import (
	"testing"
)

// The reported bug: reinstalling the agent left a second device with the same
// hostname. Enrollment always inserted a fresh row, with no way to tell "same
// machine" from "another machine".
func TestEnrollAdoptsExistingDeviceForSameMachine(t *testing.T) {
	s := newLogStore(t)

	first, adopted, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", "")
	if err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if adopted {
		t.Error("the first enrollment adopted a device; there was none to adopt")
	}

	second, adopted, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", "")
	if err != nil {
		t.Fatalf("second enroll: %v", err)
	}
	if !adopted {
		t.Error("reinstalling did not adopt the existing device")
	}
	if second.ID != first.ID {
		t.Errorf("device id changed on reinstall: %s -> %s", first.ID, second.ID)
	}

	devices, err := s.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	var matching int
	for _, d := range devices {
		if d.Hostname == "pc-a" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("hostname pc-a appears %d times, want 1", matching)
	}
}

// A fresh credential every time: adopting a record must not revive the old
// token, or a leaked one would keep working after a reinstall.
func TestEnrollRotatesTheTokenOnAdoption(t *testing.T) {
	s := newLogStore(t)

	first, _, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", "")
	if err != nil {
		t.Fatal(err)
	}
	second, adopted, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", "")
	if err != nil {
		t.Fatal(err)
	}
	if !adopted {
		t.Fatal("expected adoption")
	}
	if second.DeviceToken == first.DeviceToken {
		t.Fatal("the device token was reused across enrollments")
	}
	// The stored value is the hash, so compare through it: the new token must
	// hash to what is on the row, and the old one must not.
	var stored string
	if err := s.db.QueryRow("SELECT device_token FROM devices WHERE id = ?", second.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hashToken(second.DeviceToken) {
		t.Error("the rotated token does not match what is stored")
	}
	if stored == hashToken(first.DeviceToken) {
		t.Error("the previous token still authenticates after a reinstall")
	}
}

// The machine identifier only picks which of the owner's devices to adopt. It
// must never cross owners.
func TestEnrollDoesNotAdoptAcrossOwners(t *testing.T) {
	s := newLogStore(t)

	a, _, err := s.EnrollDevice("pc-a", "owner-1", "shared-machine", "")
	if err != nil {
		t.Fatal(err)
	}
	b, adopted, err := s.EnrollDevice("pc-a", "owner-2", "shared-machine", "")
	if err != nil {
		t.Fatal(err)
	}
	if adopted {
		t.Fatal("one owner's device was adopted by another owner")
	}
	if b.ID == a.ID {
		t.Fatal("two owners share a device record")
	}
}

// A machine identifier that isn't there must not merge unrelated devices.
func TestEnrollWithoutMachineIDAlwaysCreates(t *testing.T) {
	s := newLogStore(t)

	first, _, err := s.EnrollDevice("pc-a", "owner-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, adopted, err := s.EnrollDevice("pc-a", "owner-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if adopted {
		t.Error("adopted without a machine identifier")
	}
	if second.ID == first.ID {
		t.Error("merged two enrollments that carried no machine identifier")
	}
}

func TestEnrollAdoptionKeepsStoredFingerprint(t *testing.T) {
	s := newLogStore(t)

	if _, _, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", "fp-original"); err != nil {
		t.Fatal(err)
	}
	// A reinstall that supplies no fingerprint must not erase the stored one.
	if _, _, err := s.EnrollDevice("pc-a", "owner-1", "machine-abc", ""); err != nil {
		t.Fatal(err)
	}

	devices, err := s.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices {
		if d.Hostname == "pc-a" && d.HardwareFingerprint != "fp-original" {
			t.Errorf("fingerprint = %q, want it preserved", d.HardwareFingerprint)
		}
	}
}
