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

	// The list belongs in the dedicated column, not in the telemetry payload.
	stored, err := s.store.GetDevice(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Hardware.DockerContainers) != 0 {
		t.Fatalf("container list leaked into the telemetry payload: %+v", stored.Hardware.DockerContainers)
	}
	assertContainers := func(want int, label string) {
		t.Helper()
		state, err := s.store.GetDockerStateContainers(device.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(state) != want {
			t.Fatalf("%s: cached containers = %d, want %d", label, len(state), want)
		}
	}
	assertContainers(1, "after a sampled list")
	if state := s.getDockerState(device.ID); len(state.Containers) != 1 || state.Containers[0].Name != "web" {
		t.Fatalf("docker state = %+v", state)
	}

	// A normal telemetry cycle did not sample the list; the cache must survive.
	send(store.HardwareStats{CPUUsagePercent: 10})
	assertContainers(1, "after a non-sampling cycle")

	// A sample that listed zero containers must clear the cache.
	send(store.HardwareStats{DockerContainersSampled: true})
	assertContainers(0, "after an empty sampled list")
}
