package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sync-win/server/internal/store"
)

// The agent samples the Docker container list once a minute because it is the
// largest part of the payload. The server has to keep the last list for the
// cycles that did not include one, but a later sample that really listed zero
// containers must clear it.
func TestTelemetryKeepsDockerContainersBetweenSamples(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("telemetry@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-telemetry", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	send := func(hardware store.HardwareStats) {
		t.Helper()
		body, marshalErr := json.Marshal(telemetryRequest{DeviceToken: device.DeviceToken, Hardware: hardware})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+device.ID+"/telemetry", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleTelemetry(rec, req, device.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("telemetry status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}

	containers := []store.DockerContainer{{ID: "abc", Name: "web", Image: "nginx", State: "running"}}
	send(store.HardwareStats{DockerContainers: containers, DockerContainersSampled: true})

	// A normal telemetry cycle: the list was not sampled.
	send(store.HardwareStats{CPUUsagePercent: 10})

	stored, err := s.store.GetDevice(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Hardware.DockerContainers) != 1 || stored.Hardware.DockerContainers[0].Name != "web" {
		t.Fatalf("containers were not preserved: %+v", stored.Hardware.DockerContainers)
	}

	// A sample that listed zero containers must clear the cache.
	send(store.HardwareStats{DockerContainersSampled: true})
	stored, err = s.store.GetDevice(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Hardware.DockerContainers) != 0 {
		t.Fatalf("empty sample did not clear the list: %+v", stored.Hardware.DockerContainers)
	}
}
