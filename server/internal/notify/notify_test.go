package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	p := Providers()
	if len(p) == 0 {
		t.Fatal("no providers registered")
	}
	names := map[string]bool{}
	for _, pp := range p {
		names[pp.Name()] = true
		if pp.Name() == "" {
			t.Fatal("empty provider name")
		}
		if pp.Label() == "" {
			t.Fatalf("provider %s has no label", pp.Name())
		}
	}
	for _, want := range []string{"telegram", "webhook", "web_inbox"} {
		if !names[want] {
			t.Fatalf("missing built-in provider %q", want)
		}
	}
}

func TestLookupUnknown(t *testing.T) {
	if p := Lookup("nope"); p != nil {
		t.Fatalf("expected nil for unknown provider, got %v", p)
	}
}

func TestFormatMessageIcons(t *testing.T) {
	tests := []struct {
		event Event
		want  string
	}{
		{Event{Type: EventDeviceOffline, Hostname: "pc1"}, "🔴 [pc1] went offline"},
		{Event{Type: EventDeviceOnline, Hostname: "pc2"}, "🟢 [pc2] is back online"},
		{Event{Type: EventSyncError, Hostname: "pc3", Message: "timeout"}, "⚠️ [pc3] timeout"},
		{Event{Type: EventDeviceEnrolled, Hostname: "pc4"}, "➕ [pc4] was enrolled"},
	}
	for _, tt := range tests {
		got := FormatMessage(tt.event)
		if got != tt.want {
			t.Errorf("FormatMessage(%+v) = %q, want %q", tt.event, got, tt.want)
		}
	}
}

func TestSanitizeEvents(t *testing.T) {
	got := SanitizeEvents([]string{"device_offline", "bogus", "device_offline", "", "sync_error"})
	want := []string{"device_offline", "sync_error"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSubscribedTo(t *testing.T) {
	events := []string{"device_offline", "sync_error"}
	if !SubscribedTo(events, EventDeviceOffline) {
		t.Error("expected true for device_offline")
	}
	if SubscribedTo(events, EventDeviceOnline) {
		t.Error("expected false for device_online")
	}
}

func TestTelegramValidateRejectsEmpty(t *testing.T) {
	p := Lookup("telegram")
	if err := p.Validate(nil); err == nil {
		t.Error("expected error for nil config")
	}
	if err := p.Validate(json.RawMessage(`{"bot_token":"bad","chat_id":"x"}`)); err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestTelegramPublicFields(t *testing.T) {
	p := Lookup("telegram")
	public := p.PublicFields()
	for _, f := range public {
		if f == "bot_token" {
			t.Error("bot_token must not be in public fields")
		}
	}
	if !contains(public, "chat_id") {
		t.Error("chat_id should be in public fields")
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestTelegramValidateRejectsBadToken(t *testing.T) {
	p := Lookup("telegram")
	bad := []json.RawMessage{
		json.RawMessage(`{"bot_token":"short","chat_id":"1"}`),
		json.RawMessage(`{"bot_token":"","chat_id":"1"}`),
		json.RawMessage(`{"bot_token":"1234567890:ABCdefGHIjklMNOpqrSTUvwxYZ_1234567890","chat_id":""}`),
	}
	for _, cfg := range bad {
		if err := p.Validate(cfg); err == nil {
			t.Errorf("expected error for config %s", cfg)
		}
	}
}

func TestTelegramValidateAcceptsGood(t *testing.T) {
	p := Lookup("telegram")
	cfg := json.RawMessage(`{"bot_token":"1234567890:ABCdefGHIjklMNOpqrSTUvwxYZ_1234567890","chat_id":"9999"}`)
	if err := p.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWebhookValidateRejectsBadURL(t *testing.T) {
	p := Lookup("webhook")
	tests := []json.RawMessage{
		nil,
		json.RawMessage(`{}`),
		json.RawMessage(`{"url":"ftp://nope"}`),
		json.RawMessage(`{"url":"not-a-url"}`),
		json.RawMessage(`{"url":"http://127.0.0.1/hook"}`),
		json.RawMessage(`{"url":"http://[::1]/hook"}`),
		json.RawMessage(`{"url":"http://localhost/hook"}`),
		json.RawMessage(`{"url":"http://ok.com","secret":"` + longString() + `"}`),
	}
	for _, cfg := range tests {
		if err := p.Validate(cfg); err == nil {
			t.Errorf("expected error for config %s", cfg)
		}
	}
}

func longString() string {
	s := ""
	for i := 0; i < 300; i++ {
		s += "x"
	}
	return s
}

func TestWebhookValidateAcceptsGoodURL(t *testing.T) {
	p := Lookup("webhook")
	cfg := json.RawMessage(`{"url":"http://homeassistant.local:8123/api/notify/sync-win"}`)
	if err := p.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWebhookPostRejectsPrivateTargetBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := webhookProvider{}
	err := p.post(context.Background(), server.Client(), webhookConfig{URL: server.URL}, webhookPayload{})
	if err == nil {
		t.Fatal("private webhook target must be rejected")
	}
	if called {
		t.Fatal("private webhook target received a request")
	}
}

// TestWebhookRejectsHostResolvingToPrivateIP covers a hostname (not a literal
// IP) that resolves to a private address: it must be rejected at send time.
func TestWebhookRejectsHostResolvingToPrivateIP(t *testing.T) {
	orig := lookupIPAddr
	defer func() { lookupIPAddr = orig }()
	lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
	}

	p := webhookProvider{}
	err := p.post(context.Background(), http.DefaultClient, webhookConfig{URL: "http://internal.example.com/hook"}, webhookPayload{})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("expected private-address rejection, got %v", err)
	}
}

// TestWebhookDialerUsesPinnedIP proves the validated IP is the one dialed, so a
// hostname cannot rebind between validation and connection.
func TestWebhookDialerUsesPinnedIP(t *testing.T) {
	origLookup, origDial := lookupIPAddr, dialWebhook
	defer func() {
		lookupIPAddr = origLookup
		dialWebhook = origDial
	}()
	lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	var gotAddr string
	dialWebhook = func(_ context.Context, _ string, addr string) (net.Conn, error) {
		gotAddr = addr
		return nil, errors.New("stop after capturing address")
	}

	p := webhookProvider{}
	err := p.post(context.Background(), http.DefaultClient, webhookConfig{URL: "http://rebind.example:8123/hook"}, webhookPayload{})
	if err == nil {
		t.Fatal("expected the stubbed dialer to abort the request")
	}
	if gotAddr != "93.184.216.34:8123" {
		t.Fatalf("dialed %q, want the pinned 93.184.216.34:8123", gotAddr)
	}
}

func TestInboxValidateSound(t *testing.T) {
	p := Lookup("web_inbox")
	for _, sound := range []string{"beep", "chime", "none"} {
		cfg, _ := json.Marshal(map[string]string{"sound": sound})
		if err := p.Validate(cfg); err != nil {
			t.Errorf("sound %q: %v", sound, err)
		}
	}
	if err := p.Validate(json.RawMessage(`{"sound":"invalid"}`)); err == nil {
		t.Error("expected error for invalid sound")
	}
}
