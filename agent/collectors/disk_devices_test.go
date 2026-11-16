package collectors

import "testing"

func TestParseSmartReportTreatsADriveWithoutSmartAsUnavailable(t *testing.T) {
	// This is what a QEMU SCSI disk answers: exit 0, no smart_status block.
	report := map[string]any{
		"device":       map[string]any{"name": "/dev/sda", "type": "scsi"},
		"scsi_vendor":  "QEMU",
		"scsi_product": "QEMU HARDDISK",
	}
	health := parseSmartReport(report)
	if health.Available {
		t.Fatal("a drive without SMART data must not be reported as available")
	}
	if health.Reason == "" {
		t.Error("the reason must say why SMART is unavailable")
	}
	if health.Passed {
		t.Error("unavailable must never read as passed")
	}
}

func TestParseSmartReportReadsAnNVMeDrive(t *testing.T) {
	report := map[string]any{
		"smart_status":  map[string]any{"passed": true},
		"temperature":   map[string]any{"current": 41.0},
		"power_on_time": map[string]any{"hours": 12600.0},
		"nvme": map[string]any{
			"data_units_written": 141406.0,
			"percentage_used":    12.0,
			"self_test_log": map[string]any{
				"table": []any{
					map[string]any{
						"type":           map[string]any{"string": "Short"},
						"status":         map[string]any{"string": "Completed without error"},
						"power_on_hours": 12580.0,
					},
				},
			},
		},
	}
	health := parseSmartReport(report)
	if !health.Available || !health.Passed {
		t.Fatalf("health = %+v, want available and passed", health)
	}
	if health.TemperatureC != 41 || health.WearPercent != 12 || health.PowerOnHours != 12600 {
		t.Fatalf("parsed fields = %+v", health)
	}
	if len(health.Tests) != 1 || health.Tests[0].Result != "Completed without error" {
		t.Fatalf("tests = %+v", health.Tests)
	}
	if health.LastTestHours != 12580 {
		t.Fatalf("last test hours = %d", health.LastTestHours)
	}
}

func TestParseSmartReportReadsATAAttributes(t *testing.T) {
	report := map[string]any{
		"smart_status": map[string]any{"passed": false},
		"ata_smart_attributes": map[string]any{
			"table": []any{
				map[string]any{"id": 5.0, "raw": map[string]any{"value": 3.0}},
				map[string]any{"id": 197.0, "raw": map[string]any{"value": 2.0}},
				map[string]any{"id": 199.0, "raw": map[string]any{"value": 7.0}},
			},
		},
	}
	health := parseSmartReport(report)
	if health.Available != true || health.Passed != false {
		t.Fatalf("health = %+v", health)
	}
	if health.Reallocated != 3 || health.Pending != 2 || health.CRCErrors != 7 {
		t.Fatalf("attributes = %+v", health)
	}
}

func TestParseSmartReportReadsTopLevelNVMeKeys(t *testing.T) {
	// Current smartctl emits these at the top level, not nested under "nvme".
	report := map[string]any{
		"smart_status": map[string]any{"passed": true},
		"nvme_smart_health_information_log": map[string]any{
			"percentage_used":    7.0,
			"data_units_written": 2000.0,
			"temperature":        38.0,
		},
		"nvme_self_test_log": map[string]any{
			"table": []any{
				map[string]any{
					"type":   map[string]any{"string": "Extended"},
					"status": map[string]any{"string": "Completed without error"},
				},
			},
		},
	}
	health := parseSmartReport(report)
	if !health.Available || !health.Passed || !health.StatusKnown {
		t.Fatalf("health = %+v", health)
	}
	if health.WearPercent != 7 || health.DataWrittenBytes != 2000*512000 {
		t.Fatalf("NVMe fields = %+v", health)
	}
	if len(health.Tests) != 1 || health.Tests[0].Type != "Extended" {
		t.Fatalf("self-test log = %+v", health.Tests)
	}
}

func TestParseSmartReportWithoutAVerdictIsNotAFailure(t *testing.T) {
	report := map[string]any{
		"ata_smart_attributes": map[string]any{"table": []any{}},
	}
	health := parseSmartReport(report)
	if !health.Available {
		t.Fatalf("attributes should make the drive readable: %+v", health)
	}
	if health.StatusKnown {
		t.Fatal("no smart_status means no verdict, not a pass")
	}
	if health.Passed {
		t.Fatal("an unknown verdict must not read as passed")
	}
}
