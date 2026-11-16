package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sync-win/server/internal/store"
)

// newLiveTestServer builds a server whose live lease clock is under the test's
// control so expiry and the 15-minute ceiling can be exercised without sleeping.
func newLiveTestServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	s := newTestServer(t)
	s.liveLeases = newLiveLeaseStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	return s, &now
}

func livePost(t *testing.T, s *Server, deviceID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/"+deviceID+"/live", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleDeviceDetail(rec, req)
	return rec
}

func deviceSettingsViaAgent(t *testing.T, s *Server, deviceID, deviceToken string) (int, store.DeviceSettings, bool) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/settings", nil)
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	rec := httptest.NewRecorder()
	s.handleDeviceDetail(rec, req)
	var settings store.DeviceSettings
	var live struct {
		LiveActive bool `json:"live_active"`
	}
	if rec.Code != http.StatusOK {
		return rec.Code, settings, false
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatalf("decode live_active: %v", err)
	}
	return rec.Code, settings, live.LiveActive
}

func setStoredInterval(t *testing.T, s *Server, deviceID, sessionToken string, seconds int) {
	t.Helper()
	body := `{"display_name":"PC","tags":[],"collection_interval_seconds":` + itoa(seconds) + `}`
	req := httptest.NewRequest(http.MethodPatch, "/api/devices/"+deviceID, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleDeviceDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH settings = %d: %s", rec.Code, rec.Body.String())
	}
}

func itoa(n int) string {
	return strings.TrimSpace(strings.NewReplacer(" ", "", "\n", "").Replace(jsonNumber(n)))
}

func jsonNumber(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Activating opens a 60s lease: the settings the agent polls must report the
// 2-second cadence and live_active true, while the stored value stays untouched.
func TestLiveLeaseAcceleratesSettings(t *testing.T) {
	s, _ := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-owner@example.com")
	device, err := s.store.RegisterDevice("live-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	setStoredInterval(t, s, device.ID, sessionToken, 30)

	beforeCode, before, beforeLive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken)
	if beforeCode != http.StatusOK || before.CollectionIntervalSeconds != 30 || beforeLive {
		t.Fatalf("baseline settings = %d %+v live=%v", beforeCode, before, beforeLive)
	}

	rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Active bool  `json:"active"`
		Until  int64 `json:"until"`
	}
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &resp); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !resp.Active {
		t.Fatalf("activate response = %+v", resp)
	}
	if got, want := resp.Until, time.Now().Add(liveLeaseDuration).Unix(); got < want-2 || got > want+2 {
		t.Fatalf("until = %d, want ~%d", got, want)
	}

	code, settings, liveActive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken)
	if code != http.StatusOK || !liveActive || settings.CollectionIntervalSeconds != 2 {
		t.Fatalf("live settings = %d %+v live=%v", code, settings, liveActive)
	}

	// The stored value must survive the lease untouched.
	stored, err := s.store.GetDeviceSettings(t.Context(), device.ID)
	if err != nil || stored.CollectionIntervalSeconds != 30 {
		t.Fatalf("stored settings changed: %+v err=%v", stored, err)
	}
}

// Deactivating ends the lease immediately, even with time left on it.
func TestLiveDeactivateRestoresStoredInterval(t *testing.T) {
	s, _ := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-off@example.com")
	device, err := s.store.RegisterDevice("live-off-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	setStoredInterval(t, s, device.ID, sessionToken, 30)

	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("activate = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(livePost(t, s, device.ID, sessionToken, `{"active":false}`).Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Active {
		t.Fatalf("deactivate response = %+v", resp)
	}

	_, settings, liveActive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken)
	if liveActive || settings.CollectionIntervalSeconds != 30 {
		t.Fatalf("settings after deactivate = %+v live=%v", settings, liveActive)
	}
}

// A lease nobody renewed expires on its own: the agent falls back to the stored
// cadence without waiting for a deactivate.
func TestLiveLeaseExpiresByClock(t *testing.T) {
	s, now := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-expiry@example.com")
	device, err := s.store.RegisterDevice("live-expiry-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	setStoredInterval(t, s, device.ID, sessionToken, 30)

	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("activate = %d", rec.Code)
	}
	*now = now.Add(liveLeaseDuration + time.Second)

	_, settings, liveActive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken)
	if liveActive || settings.CollectionIntervalSeconds != 30 {
		t.Fatalf("settings after lease expiry = %+v live=%v", settings, liveActive)
	}

	// Within the same 15-minute window a fresh activate renews the lease.
	rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`)
	var resp struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Active {
		t.Fatalf("renew within window = %s", rec.Body.String())
	}
	_, settings, liveActive = deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken)
	if !liveActive || settings.CollectionIntervalSeconds != 2 {
		t.Fatalf("settings after renew = %+v live=%v", settings, liveActive)
	}
}

// The 15-minute ceiling counts from the first activation: renewals cannot push
// it forward, past it the lease is refused, and only an explicit active:false
// followed by active:true opens a new window.
func TestLiveCeilingFifteenMinutes(t *testing.T) {
	s, now := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-ceiling@example.com")
	device, err := s.store.RegisterDevice("live-ceiling-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	setStoredInterval(t, s, device.ID, sessionToken, 30)

	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("activate = %d", rec.Code)
	}
	// Repeated renewals must not move the ceiling past the first activation.
	leaseStart := *now
	var lastUntil int64
	for i := 1; i <= 15; i++ {
		*now = now.Add(liveLeaseDuration - 5*time.Second)
		rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`)
		var resp struct {
			Active bool  `json:"active"`
			Until  int64 `json:"until"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Active {
			t.Fatalf("renewal %d refused early: %s", i, rec.Body.String())
		}
		lastUntil = resp.Until
	}
	// The ceiling window is anchored at the first activation, not renewed.
	if windowEnd := leaseStart.Add(liveMaxWindow).Unix(); lastUntil > windowEnd {
		t.Fatalf("until = %d must not pass the window end %d", lastUntil, windowEnd)
	}

	*now = leaseStart.Add(liveMaxWindow + 2*time.Second) // past 15 min from first activation
	rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`)
	var refused struct {
		Active bool  `json:"active"`
		Until  int64 `json:"until"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Active {
		t.Fatalf("lease must be refused past the ceiling: %s", rec.Body.String())
	}
	if _, settings, liveActive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken); liveActive || settings.CollectionIntervalSeconds != 30 {
		t.Fatalf("settings after ceiling = %+v live=%v", settings, liveActive)
	}

	// Without an explicit deactivate the window stays closed.
	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code == http.StatusOK {
		var again struct {
			Active bool `json:"active"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &again)
		if again.Active {
			t.Fatalf("a new window must require an explicit active:false: %s", rec.Body.String())
		}
	}

	// active:false followed by active:true opens a fresh window.
	if rec := livePost(t, s, device.ID, sessionToken, `{"active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("deactivate = %d", rec.Code)
	}
	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("new window activate = %d: %s", rec.Code, rec.Body.String())
	}
	if _, settings, liveActive := deviceSettingsViaAgent(t, s, device.ID, device.DeviceToken); !liveActive || settings.CollectionIntervalSeconds != 2 {
		t.Fatalf("settings after new window = %+v live=%v", settings, liveActive)
	}
}

// Another owner must not toggle someone else's Live mode.
func TestLiveRejectsForeignOwner(t *testing.T) {
	s, _ := newLiveTestServer(t)
	ownerID, _, _ := newAuthedDevice(t, s, "live-victim@example.com")
	device, err := s.store.RegisterDevice("live-victim-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.store.CreateUser("live-foreign@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := s.store.CreateSession(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec := livePost(t, s, device.ID, otherSession.Token, `{"active":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign owner = %d, want 403", rec.Code)
	}
	if rec := livePost(t, s, device.ID, device.DeviceToken, `{"active":true}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("device token = %d, want 401 (only the owner may toggle live)", rec.Code)
	}
	if len(s.liveLeases.leases) != 0 {
		t.Fatalf("rejected requests must not open a lease: %+v", s.liveLeases.leases)
	}
}

// Unknown devices answer 404, never open a lease entry.
func TestLiveUnknownDevice(t *testing.T) {
	s, _ := newLiveTestServer(t)
	_, sessionToken, _ := newAuthedDevice(t, s, "live-unknown@example.com")
	if rec := livePost(t, s, "does-not-exist", sessionToken, `{"active":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown device = %d, want 404", rec.Code)
	}
	if len(s.liveLeases.leases) != 0 {
		t.Fatalf("unknown device must not open a lease: %+v", s.liveLeases.leases)
	}
}

// Deactivating something that was never active must stay a success.
func TestLiveDeactivateIdempotent(t *testing.T) {
	s, _ := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-idem@example.com")
	device, err := s.store.RegisterDevice("live-idem-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		rec := livePost(t, s, device.ID, sessionToken, `{"active":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("deactivate %d = %d: %s", i, rec.Code, rec.Body.String())
		}
		var resp struct {
			Active bool `json:"active"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Active {
			t.Fatalf("deactivate %d response = %+v", i, resp)
		}
	}
	if len(s.liveLeases.leases) != 0 {
		t.Fatalf("map must be empty after idempotent deactivate: %+v", s.liveLeases.leases)
	}
}

// Expired entries must not pile up: any use of the store prunes stale ones.
func TestLiveLeaseStorePrunes(t *testing.T) {
	s, now := newLiveTestServer(t)
	ownerID, sessionToken, _ := newAuthedDevice(t, s, "live-prune@example.com")
	device, err := s.store.RegisterDevice("live-prune-pc", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := livePost(t, s, device.ID, sessionToken, `{"active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("activate = %d", rec.Code)
	}
	if len(s.liveLeases.leases) != 1 {
		t.Fatalf("expected one lease entry: %+v", s.liveLeases.leases)
	}

	// Past the ceiling window plus one lease period the stale entry is
	// dropped by the next store use, so the map cannot grow unbounded.
	*now = now.Add(liveMaxWindow + liveLeaseDuration + time.Second)
	other, err := s.store.RegisterDevice("live-prune-other", "", ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	s.liveLeases.set(other.ID, false, *now) // any touch prunes
	if len(s.liveLeases.leases) != 0 {
		t.Fatalf("stale lease must be pruned: %+v", s.liveLeases.leases)
	}
}
