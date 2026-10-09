package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/coder/websocket"

	"sync-win/server/internal/tunnel"
)

// terminalAcceptOptions builds the WebSocket origin allowlist for the browser
// terminal. The default same-host check breaks under the dev Vite proxy, which
// rewrites the Host header (changeOrigin) so the browser's Origin no longer
// matches. The configured CORS origins are allowed explicitly, alongside the
// request host for same-origin deployments.
func (s *Server) terminalAcceptOptions(r *http.Request) *websocket.AcceptOptions {
	patterns := make([]string, 0, 4)
	for _, origin := range strings.Split(os.Getenv("SYNCWIN_CORS_ALLOWED_ORIGIN"), ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			patterns = append(patterns, u.Host)
		}
	}
	if r.Host != "" {
		patterns = append(patterns, r.Host)
	}
	return &websocket.AcceptOptions{OriginPatterns: patterns}
}

const (
	// remoteIdleSeconds bounds keyboard inactivity. The agent enforces the same
	// value locally; the server mirrors it in the handshake so both ends agree.
	remoteIdleSeconds = 900
	// remoteReadLimitBytes is the largest WebSocket message either side may send.
	// SSH output and pastes can exceed a single default frame, so the limit is
	// generous but finite.
	remoteReadLimitBytes = 1 << 20
)

type remoteAccessRequest struct {
	User string `json:"user"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// handleRemoteAccessRequest issues a one-time ticket for an interactive session.
// It does not carry any payload: the browser redeems the ticket on a WebSocket.
func (s *Server) handleRemoteAccessRequest(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.flags.EnableRemoteAccess {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote access is disabled"))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireOwnedDevice(w, r, deviceID) {
		return
	}
	var req remoteAccessRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	req.User = strings.TrimSpace(req.User)
	if !s.tunnelHub.AgentConnected(deviceID) {
		s.writeError(w, http.StatusConflict, errors.New("device is not connected to the tunnel"))
		return
	}
	ticket, err := s.tunnelHub.RequestSession(deviceID, req.User, remoteIdleSeconds, req.Cols, req.Rows)
	if err != nil {
		s.writeError(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ticket":               ticket,
		"user":                 req.User,
		"idle_timeout_seconds": remoteIdleSeconds,
	})
}

// handleRemoteAccessInfo reports what the dashboard needs to offer a session:
// whether the server flag is on, whether the device enabled access locally
// (reported by the agent), the default account, the accounts the agent listed,
// and whether the control tunnel is currently connected.
func (s *Server) handleRemoteAccessInfo(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireOwnedDevice(w, r, deviceID) {
		return
	}
	users, err := s.store.GetLoginUsers(r.Context(), deviceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"server_enabled": s.flags.EnableRemoteAccess,
		"device_enabled": users.Enabled,
		"default_user":   users.DefaultUser,
		"users":          users.Logins,
		"connected":      s.tunnelHub.AgentConnected(deviceID),
	})
}

// handleAgentTunnel is the agent's persistent outbound control link. The server
// pushes "open a session" messages over it; payload never travels here.
func (s *Server) handleAgentTunnel(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.flags.EnableRemoteAccess {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote access is disabled"))
		return
	}
	if !s.validDeviceToken(r, deviceID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(remoteReadLimitBytes)
	link := tunnel.NewAgentLink(conn)
	s.tunnelHub.RegisterAgent(deviceID, link)
	defer s.tunnelHub.UnregisterAgent(deviceID, link)

	for {
		if _, _, err := conn.Read(context.Background()); err != nil {
			return
		}
	}
}

// handleAgentTunnelSession attaches the byte pipe for one session. The agent
// opens it in response to an "open" message, presenting the ticket.
func (s *Server) handleAgentTunnelSession(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.flags.EnableRemoteAccess {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote access is disabled"))
		return
	}
	if !s.validDeviceToken(r, deviceID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(remoteReadLimitBytes)
	sc := tunnel.NewSessionConn(conn)
	if !s.tunnelHub.DeliverSession(ticket, deviceID, sc) {
		_ = conn.Close(websocket.StatusPolicyViolation, "unknown or expired ticket")
		return
	}
	// The browser-side relay owns Read and Write on this pipe; block until it is
	// done so this handler does not return and close the connection early.
	if d, ok := sc.(interface{ Done() <-chan struct{} }); ok {
		<-d.Done()
	}
}

// handleTerminal is the browser-facing end of a session. It authenticates with
// the dashboard session and redeems the ticket issued by handleRemoteAccessRequest.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.flags.EnableRemoteAccess {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote access is disabled"))
		return
	}
	if !s.requireOwnedDevice(w, r, deviceID) {
		return
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("missing ticket"))
		return
	}
	ownerID := s.ownerID(r)
	conn, err := websocket.Accept(w, r, s.terminalAcceptOptions(r))
	if err != nil {
		return
	}
	conn.SetReadLimit(remoteReadLimitBytes)

	// Read the target account before the ticket is consumed by AwaitSession.
	user, _ := s.tunnelHub.PendingUser(ticket, deviceID)

	ctx, cancel := context.WithTimeout(r.Context(), tunnel.InboundSessionTTL)
	defer cancel()
	agentConn, err := s.tunnelHub.AwaitSession(ctx, ticket, deviceID)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "session unavailable")
		return
	}
	// Audit the session only once it actually connects, never for a ticket that
	// was merely requested.
	session, err := s.store.StartRemoteSession(r.Context(), deviceID, ownerID, user)
	if err == nil {
		sessionID := session.ID
		// The request context is gone by the time the session ends, so closing
		// the audit row runs on a background context.
		defer func() { _ = s.store.EndRemoteSession(context.Background(), sessionID, "closed") }()
	}

	// agent -> browser: raw terminal output.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := agentConn.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(r.Context(), websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> agent: binary frames are keystrokes; a text frame is a control
	// message (a terminal resize) that travels over the control link instead.
	for {
		typ, data, err := conn.Read(r.Context())
		if err != nil {
			break
		}
		if typ == websocket.MessageText {
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
				_ = s.tunnelHub.ResizeSession(deviceID, ticket, msg.Cols, msg.Rows)
				continue
			}
		}
		if _, err := agentConn.Write(data); err != nil {
			break
		}
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	_ = agentConn.Close()
}

// handleRemoteAccessSessions returns a device's recent interactive sessions.
func (s *Server) handleRemoteAccessSessions(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireOwnedDevice(w, r, deviceID) {
		return
	}
	sessions, err := s.store.ListRemoteSessions(r.Context(), deviceID, s.ownerID(r), 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}
