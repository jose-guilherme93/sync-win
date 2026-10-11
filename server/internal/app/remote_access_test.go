package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sync-win/server/internal/logging"
	"sync-win/server/internal/tunnel"
)

func TestRemoteAccessFlagFailsClosed(t *testing.T) {
	s := newTestServer(t) // flags.EnableRemoteAccess is false by default
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/dev-1/remote-access/session", strings.NewReader(`{"user":"alice"}`))
	s.handleRemoteAccessRequest(rec, req, "dev-1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled remote access returned %d, want 503", rec.Code)
	}
}

// TestRemoteAccessEndToEnd drives the real chain: the agent opens the control
// link, the browser requests a session, the agent attaches the data pipe, and a
// byte written on the terminal echoes back. It exercises the WebSocket upgrade
// through the logging and gzip middleware, which would break if a wrapper hid
// http.Hijacker.
func TestRemoteAccessEndToEnd(t *testing.T) {
	s := newTestServer(t)
	s.flags.EnableRemoteAccess = true
	// Reproduce the dev Vite proxy: it rewrites the Host header, so the browser
	// Origin no longer matches the request host. The terminal must still accept
	// an origin from the configured allowlist.
	t.Setenv("SYNCWIN_CORS_ALLOWED_ORIGIN", "http://localhost:5173")

	user, err := s.store.CreateUser("remote@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.store.RegisterDevice("host", user.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/devices/", s.handleDeviceDetail)
	handler := securityHeadersMiddleware(corsMiddleware(csrfMiddleware(
		logging.HTTPMiddleware(s.log, gzipMiddleware(limitBody(mux))))))
	ts := httptest.NewServer(handler)
	defer ts.Close()
	wsBase := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Agent: persistent control link.
	agentCtl, _, err := websocket.Dial(ctx, wsBase+"/api/devices/"+device.ID+"/tunnel?device_token="+device.DeviceToken, nil)
	if err != nil {
		t.Fatalf("agent control dial: %v", err)
	}
	defer agentCtl.Close(websocket.StatusNormalClosure, "")

	deadline := time.Now().Add(2 * time.Second)
	for !s.tunnelHub.AgentConnected(device.ID) {
		if time.Now().After(deadline) {
			t.Fatal("agent never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Agent: on "open", attach the data pipe and echo bytes back.
	agentDone := make(chan error, 1)
	go func() {
		for {
			_, data, readErr := agentCtl.Read(ctx)
			if readErr != nil {
				agentDone <- readErr
				return
			}
			var msg tunnel.AgentMessage
			if unmarshalErr := json.Unmarshal(data, &msg); unmarshalErr != nil {
				agentDone <- unmarshalErr
				return
			}
			if msg.Type != "open" {
				continue
			}
			sc, _, dialErr := websocket.Dial(ctx, wsBase+"/api/devices/"+device.ID+"/tunnel/session?device_token="+device.DeviceToken+"&ticket="+msg.Ticket, nil)
			if dialErr != nil {
				agentDone <- dialErr
				return
			}
			for {
				_, b, sessionReadErr := sc.Read(ctx)
				if sessionReadErr != nil {
					_ = sc.Close(websocket.StatusNormalClosure, "")
					agentDone <- nil
					return
				}
				if sessionWriteErr := sc.Write(ctx, websocket.MessageBinary, b); sessionWriteErr != nil {
					agentDone <- sessionWriteErr
					return
				}
			}
		}
	}()

	// Browser: request a session ticket.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/devices/"+device.ID+"/remote-access/session", strings.NewReader(`{"user":"alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session request status = %d", resp.StatusCode)
	}
	var issued struct {
		Ticket string `json:"ticket"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&issued); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if issued.Ticket == "" {
		t.Fatal("empty ticket")
	}

	// Browser: open the terminal and expect an echo.
	term, _, err := websocket.Dial(ctx, wsBase+"/api/devices/"+device.ID+"/terminal?ticket="+issued.Ticket,
		&websocket.DialOptions{HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + session.Token},
			"Origin":        []string{"http://localhost:5173"},
		}})
	if err != nil {
		t.Fatalf("terminal dial: %v", err)
	}
	defer term.Close(websocket.StatusNormalClosure, "")

	if writeErr := term.Write(ctx, websocket.MessageBinary, []byte("hello")); writeErr != nil {
		t.Fatalf("terminal write: %v", writeErr)
	}
	_, got, err := term.Read(ctx)
	if err != nil {
		t.Fatalf("terminal read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("echo = %q, want %q", got, "hello")
	}

	// The session must be audited with the target account.
	sessions, err := s.store.ListRemoteSessions(context.Background(), device.ID, user.ID, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].User != "alice" {
		t.Fatalf("remote session not audited: %#v", sessions)
	}
}
