package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sync-win/server/internal/store"
)

// A list command asks the device for the truth now, while the agent's own
// sample of the container list is only taken about once a minute. The result
// must replace the cached snapshot before it is handed to the requester, or the
// screen that asked for a refresh would receive a fresh list and then have it
// overwritten by the next stale SSE broadcast.
func TestDockerListResultRefreshesCachedState(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("docker@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-docker", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if seedErr := s.store.UpdateDockerState(device.ID, []store.DockerContainer{
		{ID: "old", Name: "old", Image: "old", State: "exited"},
	}); seedErr != nil {
		t.Fatal(seedErr)
	}

	requestID := s.dockerQueue.EnqueueForDevice(device.ID, "list", "", "")
	message := `[{"id":"abc","names":["/web"],"image":"nginx:alpine","state":"running","status":"Up 2 minutes","created_at":1700000000,"ports":[{"private_port":80,"public_port":8080,"type":"tcp"}],"mounts":[{"type":"bind","source":"/srv","destination":"/data","rw":true}]}]`
	postDockerResult(t, s, device.ID, device.DeviceToken, requestID, "completed", message)

	cached, err := s.store.GetDockerStateContainers(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0].Name != "web" {
		t.Fatalf("cached containers = %+v", cached)
	}
	if cached[0].CreatedAt != 1700000000 {
		t.Fatalf("created_at = %d, want 1700000000", cached[0].CreatedAt)
	}
	if len(cached[0].Ports) != 1 || cached[0].Ports[0].PublicPort != 8080 {
		t.Fatalf("ports = %+v", cached[0].Ports)
	}

	state := s.getDockerState(device.ID)
	if len(state.Containers) != 1 || state.Containers[0].Name != "web" {
		t.Fatalf("docker state = %+v", state)
	}
	if state.Containers[0].CreatedAt != 1700000000 {
		t.Fatalf("state created_at = %d", state.Containers[0].CreatedAt)
	}

	// The requesting client still receives the result after the cache write.
	result := s.dockerQueue.GetAndConsumeResult(device.ID, requestID)
	if result == nil || result.Status != "completed" || result.Message != message {
		t.Fatalf("result = %+v", result)
	}
}

// A stats (or any non-list) result carries a JSON body too, but it is not a
// container list. It must not replace the snapshot.
func TestDockerNonListResultKeepsCachedState(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("docker@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-docker", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if seedErr := s.store.UpdateDockerState(device.ID, []store.DockerContainer{
		{ID: "keep", Name: "web", Image: "nginx", State: "running"},
	}); seedErr != nil {
		t.Fatal(seedErr)
	}

	requestID := s.dockerQueue.EnqueueForDevice(device.ID, "stats", "keep", "")
	postDockerResult(t, s, device.ID, device.DeviceToken, requestID, "completed",
		`{"cpu_usage_percent":12.5,"memory_percent":3.2}`)

	cached, err := s.store.GetDockerStateContainers(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0].ID != "keep" {
		t.Fatalf("stats result replaced the container cache: %+v", cached)
	}
}

// An unreadable list must leave the last good snapshot alone. The requester
// still gets its result (and the screen reports the decode failure); the cache
// must not be emptied by a malformed reply.
func TestDockerUnreadableListKeepsCachedState(t *testing.T) {
	s := newTestServer(t)
	user, err := s.store.CreateUser("docker@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("pc-docker", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if seedErr := s.store.UpdateDockerState(device.ID, []store.DockerContainer{
		{ID: "keep", Name: "web", Image: "nginx", State: "running"},
	}); seedErr != nil {
		t.Fatal(seedErr)
	}

	requestID := s.dockerQueue.EnqueueForDevice(device.ID, "list", "", "")
	postDockerResult(t, s, device.ID, device.DeviceToken, requestID, "completed", "not json")

	cached, err := s.store.GetDockerStateContainers(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0].ID != "keep" {
		t.Fatalf("unreadable list clobbered the cache: %+v", cached)
	}
	result := s.dockerQueue.GetAndConsumeResult(device.ID, requestID)
	if result == nil || result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
}

func postDockerResult(t *testing.T, s *Server, deviceID, token, requestID, status, message string) {
	t.Helper()
	body, err := json.Marshal(dockerAgentResult{
		DeviceToken: token,
		RequestID:   requestID,
		Status:      status,
		Message:     message,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+deviceID+"/docker/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleDockerPostResult(rec, req, deviceID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("docker result status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// The dashboard and the agent must agree on the request vocabulary. A type
// accepted here but rejected by the agent leaves the UI waiting for a result
// that can never arrive.
func TestDockerRequestTypeAllowlist(t *testing.T) {
	for _, reqType := range []string{
		"list", "inspect", "stats", "logs",
		"start", "stop", "restart", "kill", "remove", "pause", "unpause",
		"exec",
		"compose_read", "compose_write", "compose_up", "compose_down", "compose_ps", "compose_logs",
		"prune_system", "prune_image", "prune_container", "prune_network",
	} {
		if !validDockerRequestType(reqType) {
			t.Errorf("%q should be accepted", reqType)
		}
	}
	for _, reqType := range []string{"", "rm", "inspect_all", "PAUSE", "stats "} {
		if validDockerRequestType(reqType) {
			t.Errorf("%q should be rejected", reqType)
		}
	}
	if !dockerRequestIsReadOnly("inspect") {
		t.Error("inspect must be allowed when Docker mutations are disabled")
	}
	if dockerRequestIsReadOnly("pause") {
		t.Error("pause is a mutation")
	}
}
