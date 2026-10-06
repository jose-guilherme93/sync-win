package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sync-win/server/internal/store"
)

// inventoryFixture registers a device owned by ownerID and returns its ID plus
// the raw token, which is what the agent would authenticate with. RegisterDevice
// stores only the token hash, so the plaintext in the returned struct is the
// only copy the test can authenticate with.
func inventoryFixture(t *testing.T, s *Server, hostname, ownerID string) (string, string) {
	t.Helper()
	device, err := s.store.RegisterDevice(hostname, ownerID, ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	return device.ID, device.DeviceToken
}

// inventoryOwner creates a user and a session for it, returning both the
// owner_id a device must be registered under and the session token that reads
// it. Returning the pair removes any need for the caller to look the user up in
// a specific order.
func inventoryOwner(t *testing.T, s *Server, email string) (ownerID, sessionToken string) {
	t.Helper()
	user, err := s.store.CreateUser(email, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID, session.Token
}

func postInventory(t *testing.T, s *Server, deviceID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+deviceID+"/system-inventory", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.handleSystemInventory(rec, req, deviceID)
	return rec
}

func TestSystemInventoryUploadAndRead(t *testing.T) {
	s := newTestServer(t)
	ownerID, sessionToken := inventoryOwner(t, s, "owner-1@example.com")
	deviceID, token := inventoryFixture(t, s, "laptop", ownerID)

	rec := postInventory(t, s, deviceID, token, `{
		"device_token":"`+token+`",
		"services":[{"name":"nginx.service","status":"failed","active_state":"failed","enabled":true}],
		"ports":[{"protocol":"tcp","local_address":"","port":22,"process":"sshd","pid":812}]
	}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}

	get := func(which string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/"+which, nil)
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		rec := httptest.NewRecorder()
		if which == "services" {
			s.handleDeviceServices(rec, req, deviceID)
		} else {
			s.handleDevicePorts(rec, req, deviceID)
		}
		return rec
	}

	rec = get("services")
	if rec.Code != http.StatusOK {
		t.Fatalf("services status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var services []store.ServiceUnit
	if err := json.Unmarshal(rec.Body.Bytes(), &services); err != nil {
		t.Fatalf("services decode: %v (%s)", err, rec.Body.String())
	}
	if len(services) != 1 || services[0].Name != "nginx.service" || !services[0].Enabled {
		t.Fatalf("services = %#v", services)
	}

	rec = get("/api/devices/" + deviceID + "/ports")
	if rec.Code != http.StatusOK {
		t.Fatalf("ports status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var ports []store.OpenPort
	if err := json.Unmarshal(rec.Body.Bytes(), &ports); err != nil {
		t.Fatalf("ports decode: %v (%s)", err, rec.Body.String())
	}
	if len(ports) != 1 || ports[0].Port != 22 || ports[0].Process != "sshd" {
		t.Fatalf("ports = %#v", ports)
	}
}

func TestSystemInventoryUploadRejectsBadToken(t *testing.T) {
	s := newTestServer(t)
	deviceID, token := inventoryFixture(t, s, "laptop", "owner-1")

	if rec := postInventory(t, s, deviceID, "wrong-token", `{"services":[]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", rec.Code)
	}
	// The body token must match too: the header alone is not enough.
	if rec := postInventory(t, s, deviceID, token, `{"services":[]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing body token status = %d, want 401", rec.Code)
	}
}

func TestSystemInventoryRejectsEmptyPayload(t *testing.T) {
	s := newTestServer(t)
	deviceID, token := inventoryFixture(t, s, "laptop", "owner-1")

	// Neither section present: the agent collected nothing and there is nothing
	// to store. Accepting it would be a silent no-op.
	if rec := postInventory(t, s, deviceID, token, `{"device_token":"`+token+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// A snapshot must survive a partial upload: the agent omits a section whose tool
// failed, and the previously reported one has to stay readable.
func TestSystemInventoryPartialUploadPreservesOtherSection(t *testing.T) {
	s := newTestServer(t)
	deviceID, token := inventoryFixture(t, s, "laptop", "owner-1")

	if rec := postInventory(t, s, deviceID, token, `{
		"device_token":"`+token+`",
		"services":[{"name":"sshd.service","status":"running"}],
		"ports":[{"protocol":"tcp","port":22}]
	}`); rec.Code != http.StatusNoContent {
		t.Fatalf("first upload status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := postInventory(t, s, deviceID, token, `{
		"device_token":"`+token+`",
		"services":[{"name":"nginx.service","status":"failed"}]
	}`); rec.Code != http.StatusNoContent {
		t.Fatalf("partial upload status = %d, body = %s", rec.Code, rec.Body.String())
	}

	ports, err := s.store.GetPorts(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0].Port != 22 {
		t.Fatalf("ports section must be preserved: %#v", ports)
	}
	services, err := s.store.GetServices(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 || services[0].Name != "nginx.service" {
		t.Fatalf("services section must be replaced: %#v", services)
	}
}

// An empty array is a real report ("no services"), not an omission, so it must
// replace the previous snapshot instead of being ignored.
func TestSystemInventoryEmptySectionClearsSnapshot(t *testing.T) {
	s := newTestServer(t)
	deviceID, token := inventoryFixture(t, s, "laptop", "owner-1")

	if rec := postInventory(t, s, deviceID, token, `{"device_token":"`+token+`","services":[{"name":"sshd.service","status":"running"}]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("first upload status = %d", rec.Code)
	}
	if rec := postInventory(t, s, deviceID, token, `{"device_token":"`+token+`","services":[]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("clear upload status = %d", rec.Code)
	}
	services, err := s.store.GetServices(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 0 {
		t.Fatalf("explicit empty list must clear the snapshot: %#v", services)
	}
}

func TestDeviceInventoryReadIsOwnerScoped(t *testing.T) {
	s := newTestServer(t)
	ownerID, ownerToken := inventoryOwner(t, s, "owner-1@example.com")
	_, intruderToken := inventoryOwner(t, s, "owner-2@example.com")
	deviceID, token := inventoryFixture(t, s, "laptop", ownerID)
	if rec := postInventory(t, s, deviceID, token, `{"device_token":"`+token+`","services":[{"name":"sshd.service","status":"running"}]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("upload status = %d", rec.Code)
	}

	// token is the session token as a string; the empty case exercises the
	// anonymous path, which must be rejected before any device lookup.
	call := func(sessionToken, which string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/"+which, nil)
		if sessionToken != "" {
			req.Header.Set("Authorization", "Bearer "+sessionToken)
		}
		rec := httptest.NewRecorder()
		if which == "services" {
			s.handleDeviceServices(rec, req, deviceID)
		} else {
			s.handleDevicePorts(rec, req, deviceID)
		}
		return rec
	}

	for _, which := range []string{"services", "ports"} {
		if rec := call(ownerToken, which); rec.Code != http.StatusOK {
			t.Fatalf("owner read %s = %d", which, rec.Code)
		}
		if rec := call(intruderToken, which); rec.Code != http.StatusForbidden {
			t.Fatalf("cross-owner read %s = %d, want 403", which, rec.Code)
		}
		if rec := call("", which); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous read %s = %d, want 401", which, rec.Code)
		}
	}
}

func TestSystemInventoryRejectsWriteMethods(t *testing.T) {
	s := newTestServer(t)
	deviceID, _ := inventoryFixture(t, s, "laptop", "owner-1")

	for _, path := range []string{"services", "ports"} {
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+deviceID+"/"+path, nil)
		rec := httptest.NewRecorder()
		if path == "services" {
			s.handleDeviceServices(rec, req, deviceID)
		} else {
			s.handleDevicePorts(rec, req, deviceID)
		}
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s = %d, want 405", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/system-inventory", nil)
	rec := httptest.NewRecorder()
	s.handleSystemInventory(rec, req, deviceID)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET system-inventory = %d, want 405", rec.Code)
	}
}
