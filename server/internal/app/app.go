package app

import (
	"compress/gzip"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	syncwincrypto "sync-win/server/internal/crypto"
	"sync-win/server/internal/docker"
	"sync-win/server/internal/logging"
	"sync-win/server/internal/notify"
	"sync-win/server/internal/store"
)

const maxRequestBytes = 2 << 20

type Server struct {
	store             *store.Store
	logStore          *logging.Store
	log               *logging.Logger
	aggregator        *store.Aggregator
	notifier          *notify.Dispatcher
	dockerQueue       *docker.Queue
	dockerBroadcaster *docker.Broadcaster
	streamTickets     *streamTicketStore
	rateLimiter       *rateLimiter
	flags             featureFlags
	trustProxy        bool
}

type registerRequest struct {
	Hostname string `json:"hostname"`
	UserID   string `json:"user_id"`
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type updateEmailRequest struct {
	Email string `json:"email"`
}

type updatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type syncRequest struct {
	DeviceToken string                  `json:"device_token"`
	Preferences []store.PreferenceInput `json:"preferences"`
}

type telemetryRequest struct {
	DeviceToken string              `json:"device_token"`
	Hardware    store.HardwareStats `json:"hardware"`
	Error       string              `json:"error,omitempty"`
}

type appsRequest struct {
	DeviceToken string          `json:"device_token"`
	Apps        []store.AppInfo `json:"apps"`
}

type commandResultRequest struct {
	DeviceToken string `json:"device_token"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

type appInstallRequest struct {
	Source string `json:"source"`
	Name   string `json:"name"`
}

type heartbeatRequest struct {
	DeviceToken string `json:"device_token"`
}

type dockerRequestUI struct {
	Type    string `json:"type"`
	Target  string `json:"target"`
	Payload string `json:"payload,omitempty"`
}

type dockerAgentResult struct {
	DeviceToken string `json:"device_token"`
	RequestID   string `json:"request_id"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

func Run() error {
	root := os.Getenv("SYNCWIN_DATA_DIR")
	if root == "" {
		root = "/data"
	}
	dataStore, err := store.NewStore(root)
	if err != nil {
		return fmt.Errorf("new store: %w", err)
	}

	// Initialize structured logging system
	logCfg := logging.DefaultConfig()
	logCfg.Level = logging.ParseLevelFromEnv()
	logCfg.ConsoleLevel = logging.ParseConsoleLevelFromEnv()
	logDB := dataStore.DB()
	logStore := logging.NewStore(logDB)
	structLogger := logging.New(logCfg, logStore)
	// Stop the logger (which flushes buffered entries into the log store)
	// before closing the database; closing first dropped the final logs.
	defer func() {
		structLogger.Stop()
		_ = dataStore.Close()
	}()

	// Notification credentials are encrypted with SYNCWIN_SECRET_KEY. In
	// production the key is mandatory; in development (SYNCWIN_ENV=development)
	// we fall back to a well-known insecure key with a loud warning so the
	// project still boots out of the box without leaking secrets to disk.
	if _, err := syncwincrypto.Key(); err != nil {
		if os.Getenv("SYNCWIN_ENV") != "development" {
			return fmt.Errorf("%v — generate one with: openssl rand -hex 32", err)
		}
		os.Setenv("SYNCWIN_SECRET_KEY", "sync-win-development-only-insecure-key")
		structLogger.Warn(logging.CatSecurity, logging.EventAppStarted,
			"SYNCWIN_SECRET_KEY not set; using an insecure development key — never do this in production", nil)
	}

	aggregator := store.NewAggregator(dataStore)
	notifier := notify.NewDispatcher(dataStore, structLogger)
	dockerQueue := docker.NewQueue(30 * time.Second)
	dockerBroadcaster := docker.NewBroadcaster()
	flags := loadFeatureFlags()
	server := &Server{store: dataStore, logStore: logStore, log: structLogger, aggregator: aggregator, notifier: notifier, dockerQueue: dockerQueue, dockerBroadcaster: dockerBroadcaster, streamTickets: newStreamTicketStore(), rateLimiter: newRateLimiter(), flags: flags, trustProxy: envBool("SYNCWIN_TRUST_PROXY", false)}

	// Log application startup
	structLogger.Info(logging.CatSystem, logging.EventAppStarted,
		fmt.Sprintf("SyncWin server starting version=%s", readAgentVersion()),
		map[string]any{
			"data_dir":              root,
			"fingerprint_reconnect": flags.EnableFingerprintReconnect,
			"legacy_install":        flags.EnableLegacyInstall,
			"remote_mutations":      flags.EnableRemoteMutations,
			"docker_mutations":      flags.EnableDockerMutations,
		})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/api/auth/register", server.handleAuthRegister)
	mux.HandleFunc("/api/auth/login", server.handleAuthLogin)
	mux.HandleFunc("/api/auth/me", server.handleAuthMe)
	mux.HandleFunc("/api/auth/update-email", server.handleAuthUpdateEmail)
	mux.HandleFunc("/api/auth/update-password", server.handleAuthUpdatePassword)
	mux.HandleFunc("/api/auth/logout", server.handleAuthLogout)
	mux.HandleFunc("/api/sync-config", server.handleSyncConfig)
	mux.HandleFunc("/api/agent/version", server.handleAgentVersion)
	mux.HandleFunc("/api/devices", server.handleDevices)
	mux.HandleFunc("/api/devices/", server.handleDeviceDetail)
	mux.HandleFunc("/api/agent/install.sh", server.handleAgentInstallScript)
	mux.HandleFunc("/api/agent/download", server.handleAgentDownload)
	mux.HandleFunc("/api/agent/enroll-token", server.handleEnrollToken)
	mux.HandleFunc("/api/agent/enroll", server.handleEnroll)
	mux.HandleFunc("/api/agent/reconnect", server.handleReconnect)
	mux.HandleFunc("/api/agent/checksums", server.handleChecksums)
	mux.HandleFunc("/api/agent/signature", server.handleAgentSignature)
	mux.HandleFunc("/install/", server.handleInstallScript)

	// Logging API endpoints
	mux.HandleFunc("/api/logs", server.handleLogs)
	mux.HandleFunc("/api/logs/", server.handleLogByID)
	mux.HandleFunc("/api/logs/stats", server.handleLogStats)
	mux.HandleFunc("/api/audit", server.handleAudit)
	mux.HandleFunc("/api/errors", server.handleErrors)

	// Telemetry history v2 and retention
	mux.HandleFunc("/api/retention", server.handleRetention)
	mux.HandleFunc("/api/logs/delete", server.handleDeleteLogs)

	// Notification providers and inbox
	mux.HandleFunc("/api/notifications/config", server.handleNotificationConfig)
	mux.HandleFunc("/api/notifications/providers", server.handleNotificationProviders)
	mux.HandleFunc("/api/notifications/test", server.handleNotificationTest)
	mux.HandleFunc("/api/notifications/telegram/detect-chat", server.handleTelegramDetectChat)
	mux.HandleFunc("/api/notifications/inbox", server.handleNotificationInbox)
	mux.HandleFunc("/api/notifications/inbox/read", server.handleNotificationInboxRead)
	mux.HandleFunc("/api/notifications/stream-token", server.handleNotificationStreamToken)
	mux.HandleFunc("/api/notifications/stream", server.handleNotificationStream)

	// Serve the built dashboard from the same origin when a web build is
	// available (SYNCWIN_WEB_DIR). This avoids the slow dev-server workflow and
	// cross-origin preflights. API routes above always win over "/".
	webDir := os.Getenv("SYNCWIN_WEB_DIR")
	if webDir == "" {
		webDir = "/app/web"
	}
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		fileServer := http.FileServer(http.Dir(webDir))
		indexPage := filepath.Join(webDir, "index.html")
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				if _, err := os.Stat(filepath.Join(webDir, r.URL.Path)); err == nil {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
			http.ServeFile(w, r, indexPage)
		})
		structLogger.Info(logging.CatSystem, logging.EventAppStarted, "serving web dashboard", map[string]any{"web_dir": webDir})
	} else {
		structLogger.Warn(logging.CatSystem, logging.EventAppError, "web dashboard not found, API only", map[string]any{"web_dir": webDir})
	}

	addr := os.Getenv("SYNCWIN_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           securityHeadersMiddleware(corsMiddleware(csrfMiddleware(logging.HTTPMiddleware(structLogger, gzipMiddleware(limitBody(mux)))))),
		ReadHeaderTimeout: 5 * time.Second,
		// Bound how long a client may take to send the request body. This does
		// not affect SSE responses, which have no request body.
		ReadTimeout: 15 * time.Second,
		// No write timeout: SSE connections stay open for hours.
		WriteTimeout: 0,
		IdleTimeout:  15 * time.Second,
	}
	structLogger.Info(logging.CatSystem, logging.EventAppStarted, fmt.Sprintf("server listening on %s", addr))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	notifier.StartStatusWatcher(ctx)

	// Start Docker queue cleanup goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dockerQueue.Cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()

	// Start retention cleanup goroutine
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := logStore.Cleanup(logCfg.RetentionDays); err != nil {
					structLogger.Error(logging.CatSystem, logging.EventAppError, "retention cleanup failed", map[string]any{"error": err.Error()})
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start telemetry retention cleanup goroutine.
	// Runs once on startup (after 30s grace) then every 6 hours.
	go func() {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				if deleted, err := dataStore.RunRetentionCleanup(); err != nil {
					structLogger.Error(logging.CatSystem, logging.EventAppError, "telemetry retention cleanup failed", map[string]any{"error": err.Error()})
				} else if deleted > 0 {
					structLogger.Info(logging.CatSystem, logging.EventAppStarted, "telemetry retention cleanup completed", map[string]any{"deleted": deleted})
				}
				timer.Reset(6 * time.Hour)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start periodic WAL checkpoint goroutine.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = dataStore.CheckpointWAL()
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		structLogger.Info(logging.CatSystem, logging.EventAppShutdown, "shutting down SyncWin server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
	}
	return nil
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Attachment uploads have their own (larger) limit enforced in the
		// handler. Applying the global JSON cap here would silently clamp them
		// to 2 MB and make the advertised 8 MB unreachable.
		if r.Body != nil && !isAttachmentUpload(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// isAttachmentUpload reports whether the request is a device attachment
// upload (POST /api/devices/{id}/attachments), which enforces its own body cap.
func isAttachmentUpload(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments")
}

// gzipMiddleware compresses JSON API responses when the client advertises
// gzip support. Streaming endpoints (SSE) and non-API paths are passed
// through untouched.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/notifications/stream" ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzip.NewWriter(w)
		grw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		next.ServeHTTP(grw, r)
		if grw.compress {
			_ = gz.Close()
		}
	})
}

// gzipResponseWriter decides lazily whether a response should be compressed,
// so empty (204) and redirect responses are left alone.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	if status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified {
		g.compress = true
		g.Header().Del("Content-Length")
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Add("Vary", "Accept-Encoding")
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if !g.compress {
		return g.ResponseWriter.Write(b)
	}
	return g.gz.Write(b)
}

// Flush forwards to the underlying writer so streaming handlers keep working
// even if a response is later switched to a streaming content type.
func (g *gzipResponseWriter) Flush() {
	if g.compress && g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAgentVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	version := readAgentVersion()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
}

func readAgentVersion() string {
	// Read from file generated during Docker build (extracted from contract.json)
	if data, err := os.ReadFile("/app/agent-version.txt"); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}
	// Fallback: read from env
	if v := os.Getenv("SYNCWIN_AGENT_VERSION"); v != "" {
		return v
	}
	return "0.5.0"
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "auth-register", 10, time.Minute) {
		return
	}
	var req authRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	user, err := s.store.CreateUser(req.Email, req.Password)
	if err != nil {
		s.log.Warn(logging.CatAuth, logging.EventAuthFailed, "registration failed", map[string]any{"email": req.Email, "error": err.Error()})
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Audit(logging.CatAuth, logging.EventAuthSuccess, "user registered", map[string]any{"user_id": user.ID, "email": req.Email})
	s.writeAuth(w, r, user)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "auth-login", 10, time.Minute) {
		return
	}
	var req authRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	user, err := s.store.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		s.log.Warn(logging.CatAuth, logging.EventAuthFailed, "login failed", map[string]any{"email": req.Email, "error": err.Error()})
		s.writeError(w, http.StatusUnauthorized, err)
		return
	}
	s.writeAuth(w, r, user)
}

func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
		}
		s.writeError(w, status, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("request body must contain one JSON value")
		}
		s.writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func (s *Server) writeAuth(w http.ResponseWriter, r *http.Request, user store.User) {
	session, err := s.store.CreateSession(user.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	setSessionCookie(w, r, session.Token)
	csrfToken, err := newCSRFToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	setCSRFCookie(w, r, csrfToken)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": session.Token, "owner_id": user.ID, "email": user.Email})
}

// handleAuthMe validates an existing session so the dashboard can restore a
// login on page load without trusting stale localStorage state.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	user, err := s.store.UserByID(ownerID)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid or expired session"))
		return
	}
	if _, err := r.Cookie(csrfCookieName); err != nil {
		if csrfToken, csrfErr := newCSRFToken(); csrfErr == nil {
			setCSRFCookie(w, r, csrfToken)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"owner_id": user.ID, "email": user.Email})
}

func (s *Server) handleAuthUpdateEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var req updateEmailRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if err := s.store.UpdateUserEmail(ownerID, req.Email); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Audit(logging.CatAuth, logging.EventAuthSuccess, "profile email updated", map[string]any{"user_id": ownerID, "new_email": req.Email})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"email": req.Email})
}

func (s *Server) handleAuthUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var req updatePasswordRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if err := s.store.UpdateUserPassword(ownerID, req.CurrentPassword, req.NewPassword); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Audit(logging.CatAuth, logging.EventAuthSuccess, "profile password updated", map[string]any{"user_id": ownerID})
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := s.sessionToken(r)
	if token == "" {
		clearSessionCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.store.RevokeSession(token); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncConfig(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		dirs, err := s.store.GetSyncConfig(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		workspaceDirs, err := s.store.GetWorkspaceDirs(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"extra_dirs": dirs, "workspace_dirs": workspaceDirs})
	case http.MethodPut:
		var req struct {
			ExtraDirs     []string  `json:"extra_dirs"`
			WorkspaceDirs *[]string `json:"workspace_dirs"`
		}
		if !s.decodeBody(w, r, &req) {
			return
		}
		if err := s.store.SetSyncConfig(ownerID, req.ExtraDirs); err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.WorkspaceDirs != nil {
			if err := s.store.SetWorkspaceDirs(ownerID, *req.WorkspaceDirs); err != nil {
				s.writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeviceSyncConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	deviceID := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	deviceID = strings.TrimSuffix(deviceID, "/sync-config")
	if deviceID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("device id required"))
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("device_token")
	}
	if token == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("device token required"))
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if !s.validDeviceTokenValue(deviceID, token) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	dirs, err := s.store.GetSyncConfig(device.OwnerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	workspaceDirs, err := s.store.GetWorkspaceDirs(device.OwnerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"extra_dirs": dirs, "workspace_dirs": workspaceDirs})
}

func (s *Server) ownerID(r *http.Request) string {
	ownerID, _ := s.sessionOwner(r)
	return ownerID
}

func scheme(r *http.Request) string {
	if value := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); value == "http" || value == "https" {
		return value
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// validHostPattern restricts request hosts to characters that are legal in a
// host[:port] and cannot break out of the quoted shell assignment in the
// generated install scripts.
var validHostPattern = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]+$`)

// publicBaseURL returns the externally reachable base URL embedded in install
// scripts. SYNCWIN_PUBLIC_URL wins when set (recommended behind a reverse proxy);
// otherwise it is derived from the request. The request-derived value is
// validated so untrusted headers can never inject shell fragments into the
// generated script.
func publicBaseURL(r *http.Request) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("SYNCWIN_PUBLIC_URL")); configured != "" {
		u, err := neturl.Parse(configured)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("SYNCWIN_PUBLIC_URL must be an absolute http(s) URL")
		}
		return strings.TrimRight(configured, "/"), nil
	}
	host := strings.TrimSpace(r.Host)
	if host == "" || !validHostPattern.MatchString(host) || strings.Contains(host, "..") {
		return "", errors.New("invalid request host")
	}
	return scheme(r) + "://" + host, nil
}

// shellDoubleQuote escapes a value interpolated inside a double-quoted shell
// string so it cannot terminate the quote or expand a command.
func shellDoubleQuote(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"`", "\\`",
		`$`, `\$`,
	).Replace(value)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ownerID, ok := s.requireSession(w, r)
		if !ok {
			return
		}
		devices, err := s.store.ListDeviceSummariesForOwner(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		_ = json.NewEncoder(w).Encode(devices)
	case http.MethodPost:
		ownerID, ok := s.requireSession(w, r)
		if !ok {
			return
		}
		var req registerRequest
		if !s.decodeBody(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Hostname) == "" {
			s.writeError(w, http.StatusBadRequest, errors.New("hostname is required"))
			return
		}
		device, err := s.store.RegisterDevice(req.Hostname, req.UserID, ownerID, "")
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.log.Audit(logging.CatDevice, logging.EventDeviceRegistered, "device registered", map[string]any{"device_id": device.ID, "hostname": req.Hostname, "owner_id": ownerID})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(device)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "telemetry" {
		s.handleTelemetry(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "telemetry" && parts[2] == "history" {
		s.handleTelemetryHistory(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "telemetry" && parts[2] == "history-v2" {
		s.handleTelemetryHistoryV2(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "telemetry" && parts[2] == "delete" && r.Method == http.MethodDelete {
		s.handleDeleteDeviceTelemetry(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "heartbeat" {
		s.handleHeartbeat(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "commands" {
		s.handleCommands(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "commands" {
		if r.Method == http.MethodGet {
			s.handleCommandStatus(w, r, parts[0], parts[2])
		} else {
			s.handleCommandResult(w, r, parts[0], parts[2])
		}
		return
	}
	if len(parts) == 3 && parts[1] == "files" {
		s.handleFileDelete(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete {
		s.handleDeviceDelete(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "apps" {
		if r.Method == http.MethodPost && r.URL.Query().Get("action") == "install" {
			s.handleAppInstall(w, r, parts[0])
			return
		}
		s.handleApps(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "sync" {
		s.handleSync(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "sync-config" {
		s.handleDeviceSyncConfig(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "restore-saves" {
		s.handleRestoreSaves(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "notes" {
		s.handleDeviceNotes(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "notes" {
		s.handleDeviceNoteByID(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 2 && parts[1] == "attachments" {
		s.handleDeviceAttachments(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "attachments" {
		s.handleDeviceAttachmentByID(w, r, parts[0], parts[2])
		return
	}
	// Security audit endpoints
	if len(parts) == 3 && parts[1] == "security" && parts[2] == "audit" && r.Method == http.MethodPost {
		s.handleSecurityAudit(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "security" && parts[2] == "audits" && r.Method == http.MethodGet {
		s.handleSecurityAudits(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "security" && parts[2] == "audits" && r.Method == http.MethodGet {
		s.handleSecurityAuditDetail(w, r, parts[0], parts[3])
		return
	}
	if len(parts) == 4 && parts[1] == "security" && parts[2] == "audits" && r.Method == http.MethodDelete {
		s.handleSecurityAuditDelete(w, r, parts[0], parts[3])
		return
	}
	// Docker management endpoints (new request/response architecture)
	if len(parts) == 2 && parts[1] == "docker" && r.Method == http.MethodGet {
		s.handleDockerState(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "docker" && r.Method == http.MethodPost {
		s.handleDockerRequest(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "stream" && r.Method == http.MethodGet {
		s.handleDockerStream(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "pending" && r.Method == http.MethodGet {
		s.handleDockerPending(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "result" && r.Method == http.MethodPost {
		s.handleDockerPostResult(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "docker" && parts[2] == "result" && r.Method == http.MethodGet {
		s.handleDockerGetResult(w, r, parts[0], parts[3])
		return
	}
	if len(parts) == 2 && parts[1] == "detail" && r.Method == http.MethodGet {
		s.handleDeviceDetailFull(w, r, parts[0])
		return
	}
	// Docker sub-path endpoints: the UI posts to /docker/action, /docker/logs, etc.
	// Translate these into the canonical handleDockerRequest format.
	if len(parts) == 3 && parts[1] == "docker" && r.Method == http.MethodPost {
		s.handleDockerSubRequest(w, r, parts[0], parts[2])
		return
	}
	s.handleDeviceFiles(w, r, parts[0])
}

// handleDeviceDetailFull returns a single device with its full hardware payload
// (including logs, processes and Docker state) for the detail modal.
func (s *Server) handleDeviceDetailFull(w http.ResponseWriter, r *http.Request, deviceID string) {
	device, err := s.store.GetDeviceDetail(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(device)
}

func (s *Server) handleAppInstall(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.flags.EnableRemoteMutations {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote mutations are disabled"))
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	var req appInstallRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Source) == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("app source and name are required"))
		return
	}
	command, err := s.store.QueueInstallApp(deviceID, req.Source, req.Name)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	_ = json.NewEncoder(w).Encode(command)
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if err := s.store.DeleteDevice(deviceID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request, deviceID, fileID string) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.flags.EnableRemoteMutations {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote mutations are disabled"))
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	files, err := s.store.ListFiles(deviceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	var target *store.PreferenceFile
	for index := range files {
		if files[index].ID == fileID {
			target = &files[index]
			break
		}
	}
	if target == nil {
		s.writeError(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	if err := s.store.DeletePreferenceFile(deviceID, fileID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	command, err := s.store.QueueExcludeFile(deviceID, target.RelativePath)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.NewEncoder(w).Encode(command)
}

func isRemoteMutationCommand(cmdType string) bool {
	switch cmdType {
	case "install_app", "restore_saves", "exclude_file":
		return true
	default:
		return false
	}
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("device_token")
	}
	if !s.validDeviceTokenValue(deviceID, token) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	pending := s.store.ListPendingCommands(deviceID)
	allowed := make([]store.DeviceCommand, 0, len(pending))
	for _, cmd := range pending {
		if !s.flags.EnableRemoteMutations && isRemoteMutationCommand(cmd.Type) {
			if err := s.store.CompleteCommand(cmd.ID, deviceID, "failed", "remote mutations are disabled"); err != nil && s.log != nil {
				s.log.Warn(logging.CatCommand, logging.EventCmdRejected, "queued remote mutation rejected by feature flag", map[string]any{"command_id": cmd.ID, "device_id": deviceID, "type": cmd.Type})
			}
			continue
		}
		allowed = append(allowed, cmd)
	}
	_ = json.NewEncoder(w).Encode(allowed)
}

func (s *Server) handleCommandStatus(w http.ResponseWriter, r *http.Request, deviceID, commandID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Accept device token (stable, per-device) - this is the only auth needed
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("device_token")
	}
	if !s.validDeviceTokenValue(deviceID, token) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	cmd, err := s.store.GetCommand(commandID, deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("command not found"))
		return
	}
	_ = json.NewEncoder(w).Encode(cmd)
}

func (s *Server) handleCommandResult(w http.ResponseWriter, r *http.Request, deviceID, commandID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req commandResultRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if !s.validDeviceTokenValue(deviceID, req.DeviceToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	if req.Status != "completed" && req.Status != "failed" {
		s.writeError(w, http.StatusBadRequest, errors.New("invalid command status"))
		return
	}
	if len(req.Message) > 64<<10 {
		s.writeError(w, http.StatusRequestEntityTooLarge, errors.New("command result is too large"))
		return
	}

	// Look up command type before completing (for special post-processing)
	cmdType, _ := s.store.GetCommandType(commandID, deviceID)

	if err := s.store.CompleteCommand(commandID, deviceID, req.Status, req.Message); err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}

	// If this was a successful lynis_audit, save the report
	if cmdType == "lynis_audit" && req.Status == "completed" && req.Message != "" {
		device, err := s.store.GetDevice(deviceID)
		if err == nil {
			if _, err := s.store.SaveSecurityAudit(deviceID, device.OwnerID, req.Message); err != nil {
				s.log.Warn(logging.CatDevice, logging.EventAppError,
					"failed to save security audit",
					map[string]any{"device_id": deviceID, "error": err.Error()},
				)
			}
		}
	}

	s.log.Info(logging.CatCommand, logging.EventCommandResult, "command completed", map[string]any{"command_id": commandID, "device_id": deviceID, "status": req.Status})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreSaves(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.flags.EnableRemoteMutations {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("remote mutations are disabled"))
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	var req struct {
		PrefixID       string `json:"prefix_id"`
		GameName       string `json:"game_name"`
		TargetDeviceID string `json:"target_device_id"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	if req.PrefixID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("prefix_id is required"))
		return
	}
	if req.TargetDeviceID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("target_device_id is required"))
		return
	}
	target, err := s.store.GetDevice(req.TargetDeviceID)
	if err != nil || target.OwnerID != device.OwnerID {
		s.writeError(w, http.StatusNotFound, errors.New("target device not found"))
		return
	}
	files, err := s.store.GetSaveFilesForGame(deviceID, req.PrefixID, req.GameName)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(files) == 0 {
		s.writeError(w, http.StatusNotFound, errors.New("no save files found for this game"))
		return
	}
	command, err := s.store.QueueRestoreSavesWithFiles(target.ID, deviceID, req.PrefixID, req.GameName, files)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"command_id":    command.ID,
		"files":         len(files),
		"prefix_id":     req.PrefixID,
		"game_name":     req.GameName,
		"target_device": target.Hostname,
	})
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method == http.MethodGet {
		device, err := s.store.GetDevice(deviceID)
		if err != nil || device.OwnerID != s.ownerID(r) {
			s.writeError(w, http.StatusNotFound, errors.New("device not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(device.Apps)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req appsRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if !s.validDeviceTokenValue(deviceID, req.DeviceToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	if err := s.store.UpdateApps(deviceID, req.Apps); err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) validDeviceToken(r *http.Request, deviceID string) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("device_token")
	}
	return s.validDeviceTokenValue(deviceID, token)
}

func (s *Server) validDeviceTokenValue(deviceID, token string) bool {
	device, err := s.store.GetDevice(deviceID)
	if err != nil || token == "" || device.DeviceToken == "" {
		return false
	}
	stored := device.DeviceToken
	if isDeviceTokenDigest(stored) {
		return subtle.ConstantTimeCompare([]byte(stored), []byte(deviceTokenDigest(token))) == 1
	}
	// Compatibility for databases that have not been migrated yet. New writes
	// always store only the hash above.
	return subtle.ConstantTimeCompare([]byte(stored), []byte(token)) == 1
}

func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req telemetryRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if !s.validDeviceTokenValue(deviceID, req.DeviceToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	if req.Error != "" {
		if err := s.store.RecordSyncError(deviceID, req.Error); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.log.Warn(logging.CatSync, logging.EventSyncError, "sync error reported", map[string]any{"device_id": deviceID, "error": req.Error})
		if device, derr := s.store.GetDevice(deviceID); derr == nil {
			s.notifier.Emit(device.OwnerID, notify.Event{Type: notify.EventSyncError, DeviceID: deviceID, Hostname: device.Hostname, Message: req.Error})
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	updated, err := s.store.UpdateHardwareStats(deviceID, req.Hardware)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Append to telemetry history for chart data
	if payload, err := json.Marshal(req.Hardware); err == nil {
		_ = s.store.AppendTelemetry(deviceID, payload)
		// Feed to aggregator for down-sampling
		s.aggregator.FeedRaw(deviceID, payload, time.Now().UTC())
	}
	_ = json.NewEncoder(w).Encode(updated)

	// Broadcast Docker state via SSE if Docker data is present
	if req.Hardware.DockerAvailable {
		state := s.getDockerState(deviceID)
		s.dockerBroadcaster.Broadcast(deviceID, state)
	}
}

func (s *Server) handleTelemetryHistory(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	limit := 120
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	points, err := s.store.GetTelemetryHistory(deviceID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.NewEncoder(w).Encode(points)
}

func (s *Server) handleTelemetryHistoryV2(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}

	to := time.Now().UTC()
	from := to.Add(-7 * 24 * time.Hour) // default 1 week
	resolution := "auto"

	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, errors.New("invalid 'from' timestamp (expected RFC3339)"))
			return
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, errors.New("invalid 'to' timestamp (expected RFC3339)"))
			return
		}
		to = t
	}
	if to.Before(from) {
		s.writeError(w, http.StatusBadRequest, errors.New("'to' must not be before 'from'"))
		return
	}
	// Bound the window so an extreme range cannot force a full scan; the store
	// also caps the number of returned rows.
	if to.Sub(from) > 90*24*time.Hour {
		from = to.Add(-90 * 24 * time.Hour)
	}
	if v := r.URL.Query().Get("resolution"); v != "" {
		resolution = v
	}

	if resolution == "auto" {
		points, res, err := s.store.GetBestHistory(deviceID, from, to)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_id":  deviceID,
			"resolution": res,
			"from":       from.Format(time.RFC3339),
			"to":         to.Format(time.RFC3339),
			"points":     points,
			"count":      len(points),
		})
		return
	}

	// Specific resolution requested
	if resolution == "raw" {
		points, err := s.store.GetTelemetryHistoryRaw(deviceID, from, to)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_id":  deviceID,
			"resolution": "raw",
			"from":       from.Format(time.RFC3339),
			"to":         to.Format(time.RFC3339),
			"points":     points,
			"count":      len(points),
		})
		return
	}

	points, err := s.store.GetDownsampledRange(deviceID, resolution, from, to)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"device_id":  deviceID,
		"resolution": resolution,
		"from":       from.Format(time.RFC3339),
		"to":         to.Format(time.RFC3339),
		"points":     points,
		"count":      len(points),
	})
}

func (s *Server) handleRetention(w http.ResponseWriter, r *http.Request) {
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		rs, err := s.store.GetRetentionSettings(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rs)
	case http.MethodPut:
		var rs store.RetentionSettings
		if !s.decodeBody(w, r, &rs) {
			return
		}
		rs.OwnerID = ownerID
		if err := s.store.UpdateRetentionSettings(rs); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.log.Audit(logging.CatSystem, logging.EventAppStarted, "retention settings updated", map[string]any{
			"owner_id":  ownerID,
			"raw_hours": rs.RawHours,
			"1m_days":   rs.Resolution1mDays,
			"5m_days":   rs.Resolution5mDays,
			"1h_days":   rs.Resolution1hDays,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rs)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteDeviceTelemetry(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	if err := s.store.CleanupDeviceTelemetry(deviceID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Audit(logging.CatSystem, logging.EventAppStarted, "device telemetry deleted", map[string]any{"device_id": deviceID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	days := 5
	if v := r.URL.Query().Get("days"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			days = parsed
		}
	}
	deleted, err := s.store.CleanupOldLogsForOwner(ownerID, days)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Audit(logging.CatSystem, logging.EventAppStarted, "old logs deleted", map[string]any{"days": days, "deleted": deleted})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"deleted": deleted, "days": days})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req heartbeatRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if !s.validDeviceTokenValue(deviceID, req.DeviceToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	device, err := s.store.RecordHeartbeat(deviceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.NewEncoder(w).Encode(device)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req syncRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	if !s.validDeviceTokenValue(deviceID, req.DeviceToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	if _, err := s.store.RecordHeartbeat(deviceID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	saved, rejected, err := s.store.SavePreferenceBatch(deviceID, req.Preferences)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
		}
		s.writeError(w, status, err)
		return
	}
	if rejected == nil {
		rejected = []store.Rejection{}
	}
	s.log.Info(logging.CatSync, logging.EventSyncComplete, "sync completed", map[string]any{"device_id": deviceID, "saved": saved, "rejected": len(rejected)})
	_ = json.NewEncoder(w).Encode(map[string]any{"device_id": deviceID, "saved": saved, "rejected": rejected})
}

func (s *Server) handleDeviceFiles(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	files, err := s.store.ListFiles(deviceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.NewEncoder(w).Encode(files)
}

// allowedOrigins parses SYNCWIN_CORS_ALLOWED_ORIGIN into an allowlist. The variable
// accepts a comma-separated list because a dashboard is routinely reached under
// more than one name (localhost, 127.0.0.1, a LAN address) and a single fixed
// value only ever matched one of them, which the browser reports as a CORS
// error. Wildcards and malformed entries are dropped: reflecting an arbitrary
// origin alongside Allow-Credentials would let any site ride the session.
func allowedOrigins() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("SYNCWIN_CORS_ALLOWED_ORIGIN"))
	if raw == "" || strings.Contains(raw, "*") {
		return nil
	}
	allowed := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		parsed, err := neturl.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
			parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			continue
		}
		allowed[origin] = true
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

// setCORSHeaders answers with the origin the request actually came from, but
// only when that origin is on the allowlist. With an empty allowlist (the
// production default) the dashboard is served from the same origin and no CORS
// header is emitted at all.
func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	allowed := allowedOrigins()
	if len(allowed) > 0 {
		origin := ""
		if r != nil {
			origin = strings.TrimSpace(r.Header.Get("Origin"))
		}
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Add("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-SYNCWIN-CSRF")
}

func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	binaryPath := os.Getenv("SYNCWIN_AGENT_BINARY")
	if binaryPath == "" {
		binaryPath = "/app/sync-win-agent"
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("read agent binary: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=sync-win-agent")
	_, _ = w.Write(data)
}

func (s *Server) handleAgentInstallScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.flags.EnableLegacyInstall {
		s.writeError(w, http.StatusGone, errors.New("legacy installer is disabled"))
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	userID := r.URL.Query().Get("user")
	if userID == "" {
		userID = "$(whoami)"
	}
	hostname := r.URL.Query().Get("hostname")
	if hostname == "" {
		hostname = "$(hostname)"
	}
	serverURL, err := publicBaseURL(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	device, err := s.store.RegisterDevice(hostname, userID, ownerID, "")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
SERVER_URL="%s"
DEVICE_ID="%s"
DEVICE_TOKEN="%s"

say()  { printf '%%s\n' "$*"; }
step() { printf '\n[%%s] %%s\n' "$1" "$2"; }

step "1/5" "Downloading SyncWin agent from $SERVER_URL ..."
TARGET_USER="${SUDO_USER:-${USER:-$(whoami)}}"
TARGET_HOME="${HOME:-$(getent passwd "$TARGET_USER" | cut -d: -f6)}"
mkdir -p "$TARGET_HOME/.local/bin"
agent_tmp="$(mktemp "$TARGET_HOME/.local/bin/.sync-win-agent.XXXXXX")"
trap 'rm -f "$agent_tmp"' EXIT
if ! curl --fail --silent --show-error --location "$SERVER_URL/api/agent/download" -o "$agent_tmp"; then
  say ""
  say "  ERROR - could not download the agent from $SERVER_URL"
  say "  Check that THIS machine can reach that address (IP, firewall, port)."
  exit 1
fi

step "2/5" "Installing agent binary to $TARGET_HOME/.local/bin/sync-win-agent"
chmod 755 "$agent_tmp"
mv -f "$agent_tmp" "$TARGET_HOME/.local/bin/sync-win-agent"

step "3/5" "Creating systemd user service (sync-win-agent.service)"
mkdir -p "$TARGET_HOME/.config/systemd/user"
cat > "$TARGET_HOME/.config/systemd/user/sync-win-agent.service" <<UNIT
[Unit]
Description=SyncWin agent
After=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
ExecStart=${TARGET_HOME}/.local/bin/sync-win-agent daemon --server $SERVER_URL --device-id $DEVICE_ID --device-token $DEVICE_TOKEN
Restart=always
RestartSec=15

[Install]
WantedBy=default.target
UNIT
systemctl --user daemon-reload

step "4/5" "Starting service for user $TARGET_USER"
systemctl --user enable sync-win-agent.service > /dev/null
systemctl --user restart sync-win-agent.service
if [ "\${SYNCWIN_AUTO_UPDATE:-1}" = "1" ]; then
  cat > "$TARGET_HOME/.config/systemd/user/sync-win-agent-update.service" <<UNIT
[Unit]
Description=SyncWin Agent automatic update
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=${TARGET_HOME}/.local/bin/sync-win-agent update --server $SERVER_URL --service sync-win-agent.service --user-systemd
UNIT
  cat > "$TARGET_HOME/.config/systemd/user/sync-win-agent-update.timer" <<UNIT
[Unit]
Description=Check for SyncWin Agent updates

[Timer]
OnBootSec=5min
OnUnitActiveSec=15min
Persistent=true

[Install]
WantedBy=timers.target
UNIT
  systemctl --user daemon-reload
  systemctl --user enable --now sync-win-agent-update.timer > /dev/null 2>&1 || true
fi
loginctl enable-linger "$TARGET_USER" > /dev/null 2>&1 || true

step "5/5" "Verifying connection to the server (up to 20 seconds)"
ok=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl --fail --silent --show-error -X POST \
      "$SERVER_URL/api/devices/$DEVICE_ID/heartbeat" \
      -H 'Content-Type: application/json' \
      -d "{\"device_token\":\"$DEVICE_TOKEN\"}" > /dev/null 2>&1; then
    ok=1
    break
  fi
  printf '.'
  sleep 2
done
printf '\n\n'

if [ "$ok" = "1" ]; then
  say "  SUCCESS - SyncWin agent installed AND connected."
  say "  Device '$(hostname)' (user: $TARGET_USER, id: $DEVICE_ID)"
  say "  should already appear on your web dashboard."
  say ""
  say "  Useful commands:"
  say "    journalctl --user -u sync-win-agent -f      # follow live logs"
  say "    systemctl --user restart sync-win-agent     # restart after changes"
else
  say "  ATTENTION - the agent is installed and running, but it could NOT reach the server:"
  say "    $SERVER_URL"
  say ""
  say "  Check on THIS machine:"
  say "    1. Is the server reachable?"
  say "       curl -fsSL $SERVER_URL/api/devices -o /dev/null && echo reachable"
  say "    2. Use the Docker HOST LAN IP (container-internal IPs like 172.x do not work)."
  say "    3. A firewall or router may be blocking the port."
  say ""
  say "  Live logs:   journalctl --user -u sync-win-agent -e"
  say "  After fixing the network/address:"
  say "               systemctl --user restart sync-win-agent"
  exit 1
fi
`, shellDoubleQuote(serverURL), device.ID, device.DeviceToken)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

type enrollTokenRequest struct{}

func (s *Server) handleEnrollToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "enroll-token", 20, time.Minute) {
		return
	}
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	token, err := s.store.CreateEnrollmentToken(ownerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info(logging.CatAgent, logging.EventAgentEnrollStart, "enrollment token created", map[string]any{"owner_id": ownerID})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/install/")
	if token == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("enrollment token required"))
		return
	}
	if _, err := s.store.ValidateEnrollmentToken(token); err != nil {
		s.writeError(w, http.StatusUnauthorized, err)
		return
	}
	serverURL, err := publicBaseURL(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -Eeuo pipefail

SYNCWIN_SERVER="%s"
SYNCWIN_TOKEN="%s"

`, shellDoubleQuote(serverURL), token)
	// Append the static installer from the embedded filesystem.
	data, err := os.ReadFile("/app/install.sh")
	if err != nil {
		// Fallback: serve the inline installer.
		data = []byte(installerScript)
	}
	script += string(data)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

type enrollRequest struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Kernel       string `json:"kernel"`
	AgentVersion string `json:"agent_version"`
	Fingerprint  string `json:"fingerprint"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "enroll", 30, time.Minute) {
		return
	}
	var req enrollRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	ownerID, err := s.store.ValidateEnrollmentToken(req.Token)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, err)
		return
	}
	hostname := req.Hostname
	if hostname == "" {
		hostname = "unknown"
	}
	device, err := s.store.RegisterDevice(hostname, "", ownerID, req.Fingerprint)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.ConsumeEnrollmentToken(req.Token, device.ID); err != nil {
		prefix := req.Token
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		s.log.Warn(logging.CatAgent, logging.EventAgentEnrollFailed, "enrollment token consume failed", map[string]any{"token_prefix": prefix, "error": err.Error()})
	}
	s.log.Audit(logging.CatAgent, logging.EventAgentEnrollComplete, "device enrolled", map[string]any{"device_id": device.ID, "hostname": hostname, "architecture": req.Architecture})
	s.notifier.Emit(ownerID, notify.Event{Type: notify.EventDeviceEnrolled, DeviceID: device.ID, Hostname: hostname, Message: "was enrolled"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"device_id": device.ID, "device_token": device.DeviceToken})
}

type reconnectRequest struct {
	Fingerprint string `json:"fingerprint"`
	Hostname    string `json:"hostname"`
}

type reconnectResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	Reconnected bool   `json:"reconnected"`
}

func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	// A hardware fingerprint is observable metadata, not an authenticator.
	// Recovery therefore always requires a fresh, single-use enrollment token.
	s.writeError(w, http.StatusGone, errors.New("fingerprint reconnect is permanently disabled; enroll again"))
}

func (s *Server) handleChecksums(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := os.ReadFile("/app/checksums.txt")
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("checksums not available"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

// handleAgentSignature serves the detached Ed25519 signature of the agent
// binary. The agent verifies it against the public key embedded at build time
// before installing an update, so a compromised or spoofed server cannot ship
// an arbitrary binary.
func (s *Server) handleAgentSignature(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := os.Getenv("SYNCWIN_AGENT_SIGNATURE")
	if path == "" {
		path = "/app/sync-win-agent.sig"
	}
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		s.writeError(w, http.StatusNotFound, errors.New("agent signature not available"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

// installerScript is a fallback embedded copy of the SyncWin agent installer.
// When the server can read /app/install.sh from disk, it uses that instead.
const installerScript = `# SyncWin agent installer (fallback — see scripts/install.sh for full version)
echo "ERROR: install.sh not found on server. Deploy scripts/install.sh to /app/install.sh."
exit 1
`

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	message := err.Error()
	if status >= http.StatusInternalServerError {
		message = "internal server error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// =============================================================================
// Logging API endpoints
// =============================================================================

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	params := logging.QueryParams{
		OwnerID:       ownerID,
		Level:         r.URL.Query().Get("level"),
		Category:      r.URL.Query().Get("category"),
		Event:         r.URL.Query().Get("event"),
		DeviceID:      r.URL.Query().Get("device_id"),
		UserID:        r.URL.Query().Get("user_id"),
		RequestID:     r.URL.Query().Get("request_id"),
		CorrelationID: r.URL.Query().Get("correlation_id"),
		From:          r.URL.Query().Get("from"),
		To:            r.URL.Query().Get("to"),
		Search:        r.URL.Query().Get("search"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			params.Offset = n
		}
	}

	result, err := s.logStore.Query(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleLogByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	// Extract ID from path: /api/logs/{id}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	if idStr == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("log id required"))
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errors.New("invalid log id"))
		return
	}

	entry, err := s.logStore.GetByIDForOwner(id, ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusNotFound, errors.New("log entry not found"))
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entry)
}

func (s *Server) handleLogStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	stats, err := s.logStore.StatsForOwner(ownerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}

	result, err := s.logStore.GetAuditTrailForOwner(ownerID, limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}

	result, err := s.logStore.GetErrorsForOwner(ownerID, limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ──────────────────────────────────────────────────────────────────────────────
// Device notes
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleDeviceNotes(w http.ResponseWriter, r *http.Request, deviceID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		notes, err := s.store.GetDeviceNotes(deviceID, s.ownerID(r))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(notes)
	case http.MethodPost:
		var req struct {
			Content string `json:"content"`
		}
		if !s.decodeBody(w, r, &req) {
			return
		}
		note, err := s.store.CreateDeviceNote(deviceID, s.ownerID(r), req.Content)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(note)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeviceNoteByID(w http.ResponseWriter, r *http.Request, deviceID, noteID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if !s.decodeBody(w, r, &req) {
			return
		}
		if err := s.store.UpdateDeviceNote(noteID, s.ownerID(r), req.Content); err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeleteDeviceNote(noteID, s.ownerID(r)); err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Device attachments
// ──────────────────────────────────────────────────────────────────────────────

const maxAttachmentSize = 8 << 20 // 8 MB

// inlineSafeMimeType returns a browser-safe content type for serving an
// attachment inline, or "" when it must be downloaded as an opaque stream.
// The type is derived from the bytes and never trusted from the uploader, so
// an authenticated user cannot store HTML/JS/SVG and have it execute on the
// dashboard origin.
func inlineSafeMimeType(data []byte) string {
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	switch detected := http.DetectContentType(sample); detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected
	default:
		return ""
	}
}

func (s *Server) handleDeviceAttachments(w http.ResponseWriter, r *http.Request, deviceID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		atts, err := s.store.GetDeviceAttachments(deviceID, s.ownerID(r))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(atts)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+1024)
		if err := r.ParseMultipartForm(maxAttachmentSize); err != nil {
			s.writeError(w, http.StatusBadRequest, errors.New("file too large (max 8 MB)"))
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			s.writeError(w, http.StatusBadRequest, errors.New("no file provided"))
			return
		}
		defer file.Close()
		if header.Size > maxAttachmentSize {
			s.writeError(w, http.StatusBadRequest, errors.New("file too large (max 8 MB)"))
			return
		}
		data, err := io.ReadAll(file)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		mimeType := inlineSafeMimeType(data)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		caption := r.FormValue("caption")
		att, err := s.store.CreateDeviceAttachment(deviceID, s.ownerID(r), header.Filename, mimeType, caption, data)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		// Optionally create a note alongside the attachment
		if noteContent := r.FormValue("content"); noteContent != "" {
			_, _ = s.store.CreateDeviceNote(deviceID, s.ownerID(r), noteContent)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(att)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeviceAttachmentByID(w http.ResponseWriter, r *http.Request, deviceID, attID string) {
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if device.OwnerID != ownerID {
		s.writeError(w, http.StatusForbidden, errors.New("not authorized"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		att, data, err := s.store.GetDeviceAttachmentData(attID, ownerID)
		if err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		contentType := att.MimeType
		disposition := "attachment"
		if safe := inlineSafeMimeType(data); safe != "" {
			contentType = safe
			disposition = "inline"
		} else if contentType != "application/octet-stream" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if cd := mime.FormatMediaType(disposition, map[string]string{"filename": att.Filename}); cd != "" {
			w.Header().Set("Content-Disposition", cd)
		} else {
			w.Header().Set("Content-Disposition", disposition)
		}
		// Attachments are user-controlled: forbid scripts and subresources so a
		// stored HTML/JS/SVG payload can never execute on the dashboard origin.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Content-Length", strconv.FormatInt(att.SizeBytes, 10))
		_, _ = w.Write(data)
	case http.MethodPut:
		var req struct {
			Caption string `json:"caption"`
		}
		if !s.decodeBody(w, r, &req) {
			return
		}
		if err := s.store.UpdateDeviceAttachmentCaption(attID, ownerID, req.Caption); err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeleteDeviceAttachment(attID, ownerID); err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Notification API
// ──────────────────────────────────────────────────────────────────────────────

type notificationConfigRequest struct {
	Provider string          `json:"provider"`
	Enabled  bool            `json:"enabled"`
	Events   []string        `json:"events"`
	Config   json.RawMessage `json:"config"`
}

// maskedConfig returns the config with non-public fields replaced by "***".
func maskedConfig(raw json.RawMessage, publicFields []string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return json.RawMessage(`{}`)
	}
	public := map[string]bool{}
	for _, f := range publicFields {
		public[f] = true
	}
	for k := range all {
		if !public[k] {
			all[k] = "***"
		}
	}
	out, _ := json.Marshal(all)
	return out
}

// mergeConfigs combines existing stored config with incoming updates. Fields in
// incoming that are masked ("***") are replaced with their stored values.
func mergeConfigs(existing, incoming json.RawMessage) json.RawMessage {
	var stored map[string]any
	if err := json.Unmarshal(existing, &stored); err != nil {
		return incoming
	}
	var incomingMap map[string]any
	if err := json.Unmarshal(incoming, &incomingMap); err != nil {
		return incoming
	}
	for k, v := range incomingMap {
		if s, ok := v.(string); ok && s == "***" {
			if storedVal, exists := stored[k]; exists {
				incomingMap[k] = storedVal
			}
		}
	}
	out, err := json.Marshal(incomingMap)
	if err != nil {
		return incoming
	}
	return out
}

// notificationConfigResponse is the payload returned to the dashboard for
// GET /api/notifications/config. Secrets are masked; configured indicates
// whether a config has been saved (even if masked).
type notificationConfigResponse struct {
	Provider   string          `json:"provider"`
	Enabled    bool            `json:"enabled"`
	Events     []string        `json:"events"`
	Config     json.RawMessage `json:"config"`
	Configured bool            `json:"configured"`
}

func (s *Server) handleNotificationConfig(w http.ResponseWriter, r *http.Request) {
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		configs, err := s.store.ListNotificationConfigs(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		// Always include web_inbox in the response even if no row exists yet.
		result := make([]notificationConfigResponse, 0, len(configs))
		seen := map[string]bool{}
		for _, cfg := range configs {
			seen[cfg.Provider] = true
			p := notify.Lookup(cfg.Provider)
			publicFields := []string{}
			if p != nil {
				publicFields = p.PublicFields()
			}
			result = append(result, notificationConfigResponse{
				Provider:   cfg.Provider,
				Enabled:    cfg.Enabled,
				Events:     cfg.Events,
				Config:     maskedConfig(cfg.Config, publicFields),
				Configured: true,
			})
		}
		// Ensure web_inbox is always present.
		if !seen["web_inbox"] {
			result = append(result, notificationConfigResponse{
				Provider:   "web_inbox",
				Enabled:    true,
				Events:     []string{},
				Config:     json.RawMessage(`{"sound":"beep"}`),
				Configured: false,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)

	case http.MethodPut:
		var req notificationConfigRequest
		if !s.decodeBody(w, r, &req) {
			return
		}
		p := notify.Lookup(req.Provider)
		if p == nil {
			s.writeError(w, http.StatusBadRequest, errors.New("unknown notification provider"))
			return
		}
		events := notify.SanitizeEvents(req.Events)
		if req.Config == nil {
			req.Config = json.RawMessage(`{}`)
		}
		// Merge incoming config with existing stored config so that masked
		// fields (e.g. bot_token) are preserved when the frontend sends "***".
		existing, _ := s.store.GetNotificationConfig(ownerID, req.Provider)
		if existing != nil && len(existing.Config) > 0 {
			req.Config = mergeConfigs(existing.Config, req.Config)
		}
		if err := p.Validate(req.Config); err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.store.SaveNotificationConfig(ownerID, req.Provider, req.Enabled, events, req.Config); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.log.Audit(logging.CatSystem, logging.EventDeviceUpdated,
			"notification config updated",
			map[string]any{"owner_id": ownerID, "provider": req.Provider, "enabled": req.Enabled},
		)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNotificationProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	providers := notify.Providers()
	type providerInfo struct {
		Name        string `json:"name"`
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	list := make([]providerInfo, len(providers))
	for i, p := range providers {
		list[i] = providerInfo{Name: p.Name(), Label: p.Label(), Description: p.Description()}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

type notificationTestRequest struct {
	Provider string          `json:"provider"`
	Config   json.RawMessage `json:"config"`
}

func (s *Server) handleNotificationTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "notification-test", 10, time.Minute) {
		return
	}
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	var req notificationTestRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	config := req.Config
	if config == nil {
		// Use the saved config for this provider.
		saved, err := s.store.GetNotificationConfig(ownerID, req.Provider)
		if err != nil {
			s.log.Warn(logging.CatSystem, logging.EventNotifyTest,
				"notification test: failed to load saved config",
				map[string]any{"owner_id": ownerID, "provider": req.Provider, "error": err.Error()},
			)
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if saved == nil {
			s.log.Warn(logging.CatSystem, logging.EventNotifyTest,
				"notification test: no config saved for provider",
				map[string]any{"owner_id": ownerID, "provider": req.Provider},
			)
			s.writeError(w, http.StatusBadRequest, errors.New("no configuration saved for this provider"))
			return
		}
		config = saved.Config
		s.log.Info(logging.CatSystem, logging.EventNotifyTest,
			"notification test: loaded saved config",
			map[string]any{"owner_id": ownerID, "provider": req.Provider},
		)
	}
	if err := s.notifier.TestProvider(r.Context(), req.Provider, config); err != nil {
		s.log.Warn(logging.CatSystem, logging.EventNotifyTest,
			"notification test: provider returned error",
			map[string]any{"owner_id": ownerID, "provider": req.Provider, "error": err.Error()},
		)
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Audit(logging.CatSystem, logging.EventNotifyTest,
		"notification test sent successfully",
		map[string]any{"owner_id": ownerID, "provider": req.Provider},
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// POST /api/notifications/telegram/detect-chat — auto-detect the user's chat_id
// by calling the Telegram getUpdates API with the provided bot_token.
func (s *Server) handleTelegramDetectChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allowRate(w, r, "telegram-detect", 10, time.Minute) {
		return
	}
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	var req struct {
		BotToken string `json:"bot_token"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	if req.BotToken == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("bot_token is required"))
		return
	}
	chatID, err := notify.DetectChatID(r.Context(), s.notifier.HTTPClient(), req.BotToken)
	if err != nil {
		s.log.Warn(logging.CatSystem, logging.EventNotifyTest,
			"telegram detect-chat failed",
			map[string]any{"owner_id": ownerID, "error": err.Error()},
		)
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Audit(logging.CatSystem, logging.EventNotifyTest,
		"telegram detect-chat succeeded",
		map[string]any{"owner_id": ownerID, "chat_id": chatID},
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"chat_id": chatID})
}

func (s *Server) handleNotificationInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	var sinceID int64
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			sinceID = n
		}
	}
	events, err := s.store.ListNotificationEvents(ownerID, sinceID, 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

func (s *Server) handleNotificationInboxRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID := s.ownerID(r)
	if ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	if err := s.store.MarkNotificationEventsRead(ownerID, req.IDs); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNotificationStreamToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerID, ok := s.requireSession(w, r)
	if !ok || s.streamTickets == nil {
		if ok {
			s.writeError(w, http.StatusServiceUnavailable, errors.New("stream tickets unavailable"))
		}
		return
	}
	ticket, err := s.streamTickets.issue(ownerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ticket": ticket})
}

// handleNotificationStream is an SSE endpoint that streams notification events in real-time.
func (s *Server) handleNotificationStream(w http.ResponseWriter, r *http.Request) {

	ownerID := s.ownerID(r)
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ownerID == "" || s.streamTickets == nil || !s.streamTickets.consume(ticket, ownerID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid stream ticket"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.notifier.NotifBroadcaster().Subscribe(ownerID)
	defer s.notifier.NotifBroadcaster().Unsubscribe(ownerID, ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- Docker Management Handlers ---

func dockerRequestIsReadOnly(reqType string) bool {
	switch reqType {
	case "list", "stats", "logs", "compose_read", "compose_ps", "compose_logs":
		return true
	default:
		return false
	}
}

// enqueueDockerRequest validates ownership, validates the type, enqueues, and returns the ID.
func (s *Server) enqueueDockerRequest(w http.ResponseWriter, r *http.Request, deviceID string, req dockerRequestUI) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if req.Type == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("type is required"))
		return
	}
	validTypes := map[string]bool{
		"list": true, "stats": true, "logs": true,
		"start": true, "stop": true, "restart": true, "kill": true, "remove": true,
		"exec":         true,
		"compose_read": true, "compose_write": true,
		"compose_up": true, "compose_down": true, "compose_ps": true, "compose_logs": true,
		"prune_system": true, "prune_image": true, "prune_container": true, "prune_network": true,
	}
	if !validTypes[req.Type] {
		s.writeError(w, http.StatusBadRequest, errors.New("unsupported docker type"))
		return
	}
	if !s.flags.EnableDockerMutations && !dockerRequestIsReadOnly(req.Type) {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("Docker mutations are disabled"))
		return
	}
	reqID := s.dockerQueue.EnqueueForDevice(deviceID, req.Type, req.Target, req.Payload)
	s.log.Info(logging.CatCommand, logging.EventCommandResult,
		"docker request queued",
		map[string]any{"device_id": deviceID, "type": req.Type, "target": req.Target, "request_id": reqID},
	)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": reqID})
}

// handleDockerSubRequest translates UI sub-path requests (/docker/action, /docker/logs, etc.)
// into the canonical Docker request format and enqueues them.
func (s *Server) handleDockerSubRequest(w http.ResponseWriter, r *http.Request, deviceID, subPath string) {
	var body struct {
		Action      string `json:"action,omitempty"`
		ContainerID string `json:"container_id,omitempty"`
		Command     any    `json:"command,omitempty"`
		Path        string `json:"path,omitempty"`
		Content     string `json:"content,omitempty"`
		Tail        int    `json:"tail,omitempty"`
		Target      string `json:"target,omitempty"`
	}
	if !s.decodeBody(w, r, &body) {
		return
	}

	var req dockerRequestUI
	switch subPath {
	case "action":
		req.Type = body.Action
		req.Target = body.ContainerID
	case "logs":
		req.Type = "logs"
		req.Target = body.ContainerID
		if body.Tail > 0 {
			req.Payload = fmt.Sprintf("%d", body.Tail)
		}
	case "exec":
		req.Type = "exec"
		req.Target = body.ContainerID
		if body.Command != nil {
			data, _ := json.Marshal(body.Command)
			req.Payload = string(data)
		}
	case "compose":
		switch body.Action {
		case "read":
			req.Type = "compose_read"
		case "write":
			req.Type = "compose_write"
			req.Target = body.Path
			req.Payload = body.Content
		case "up":
			req.Type = "compose_up"
			req.Target = body.Path
		case "down":
			req.Type = "compose_down"
			req.Target = body.Path
		case "ps":
			req.Type = "compose_ps"
			req.Target = body.Path
		case "logs":
			req.Type = "compose_logs"
			req.Target = body.Path
		default:
			s.writeError(w, http.StatusBadRequest, errors.New("unsupported compose action"))
			return
		}
	case "prune":
		target := body.Target
		if target == "" {
			target = "system"
		}
		req.Type = "prune_" + target
	default:
		s.writeError(w, http.StatusBadRequest, errors.New("unsupported docker sub-path"))
		return
	}

	s.enqueueDockerRequest(w, r, deviceID, req)
}

// handleDockerList queues a docker_list_containers command and returns the command ID
// so the client can poll for results.
// =============================================================================
// Docker management — request/response + SSE architecture
// =============================================================================

// handleDockerRequest queues a Docker operation requested by the UI.
// Returns the request ID so the UI can poll for the result.
func (s *Server) handleDockerRequest(w http.ResponseWriter, r *http.Request, deviceID string) {
	var req dockerRequestUI
	if !s.decodeBody(w, r, &req) {
		return
	}
	s.enqueueDockerRequest(w, r, deviceID, req)
}

// handleDockerGetResult returns the result of a Docker request.
func (s *Server) handleDockerGetResult(w http.ResponseWriter, r *http.Request, deviceID, requestID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	result := s.dockerQueue.GetAndConsumeResult(deviceID, requestID)
	if result == nil {
		w.WriteHeader(http.StatusAccepted) // 202 = still pending
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleDockerState returns cached Docker state from the latest telemetry.
func (s *Server) handleDockerState(w http.ResponseWriter, r *http.Request, deviceID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	state := docker.DockerState{
		Available:  device.Hardware.DockerAvailable,
		Containers: make([]docker.DockerSummary, 0),
	}
	for _, c := range device.Hardware.DockerContainers {
		summary := docker.DockerSummary{
			ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Status: c.Status,
		}
		for _, p := range c.Ports {
			summary.Ports = append(summary.Ports, docker.PortMapping{
				PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type, IP: p.IP,
			})
		}
		for _, m := range c.Mounts {
			summary.Mounts = append(summary.Mounts, docker.MountInfo{
				Type: m.Type, Source: m.Source, Destination: m.Destination, RW: m.RW,
			})
		}
		state.Containers = append(state.Containers, summary)
	}
	if device.Hardware.DockerInfo != nil {
		state.Info = &docker.DockerInfoData{
			Version: device.Hardware.DockerInfo.Version, Total: device.Hardware.DockerInfo.Total,
			Running: device.Hardware.DockerInfo.Running, Stopped: device.Hardware.DockerInfo.Stopped,
			Paused: device.Hardware.DockerInfo.Paused, Images: device.Hardware.DockerInfo.Images,
			Driver: device.Hardware.DockerInfo.Driver, NCPU: device.Hardware.DockerInfo.NCPU,
		}
	}
	_ = json.NewEncoder(w).Encode(state)
}

// handleDockerStream is an SSE endpoint that streams Docker state updates.
func (s *Server) handleDockerStream(w http.ResponseWriter, r *http.Request, deviceID string) {
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	_ = device

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.dockerBroadcaster.Subscribe(deviceID)
	defer s.dockerBroadcaster.Unsubscribe(deviceID, ch)

	// Send current state immediately
	currentState := s.getDockerState(deviceID)
	if data, err := json.Marshal(currentState); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// getDockerState builds a DockerState from the device's latest telemetry.
func (s *Server) getDockerState(deviceID string) docker.DockerState {
	device, err := s.store.GetDevice(deviceID)
	if err != nil {
		return docker.DockerState{}
	}
	state := docker.DockerState{
		Available:  device.Hardware.DockerAvailable,
		Containers: make([]docker.DockerSummary, 0),
	}
	for _, c := range device.Hardware.DockerContainers {
		summary := docker.DockerSummary{
			ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Status: c.Status,
		}
		for _, p := range c.Ports {
			summary.Ports = append(summary.Ports, docker.PortMapping{
				PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type, IP: p.IP,
			})
		}
		for _, m := range c.Mounts {
			summary.Mounts = append(summary.Mounts, docker.MountInfo{
				Type: m.Type, Source: m.Source, Destination: m.Destination, RW: m.RW,
			})
		}
		state.Containers = append(state.Containers, summary)
	}
	if device.Hardware.DockerInfo != nil {
		state.Info = &docker.DockerInfoData{
			Version: device.Hardware.DockerInfo.Version, Total: device.Hardware.DockerInfo.Total,
			Running: device.Hardware.DockerInfo.Running, Stopped: device.Hardware.DockerInfo.Stopped,
			Paused: device.Hardware.DockerInfo.Paused, Images: device.Hardware.DockerInfo.Images,
			Driver: device.Hardware.DockerInfo.Driver, NCPU: device.Hardware.DockerInfo.NCPU,
		}
	}
	return state
}

// handleDockerPending returns all pending Docker requests for a device (agent-facing).
func (s *Server) handleDockerPending(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.validDeviceToken(r, deviceID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	requests := s.dockerQueue.DequeueAll(deviceID)
	if requests == nil {
		requests = make([]*docker.Request, 0)
	}
	_ = json.NewEncoder(w).Encode(requests)
}

// handleDockerPostResult stores a Docker result from the agent and broadcasts to SSE subscribers.
func (s *Server) handleDockerPostResult(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.validDeviceToken(r, deviceID) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid device token"))
		return
	}
	var req dockerAgentResult
	if !s.decodeBody(w, r, &req) {
		return
	}
	if req.Status != "completed" && req.Status != "failed" {
		s.writeError(w, http.StatusBadRequest, errors.New("invalid docker result status"))
		return
	}
	if len(req.Message) > 64<<10 {
		s.writeError(w, http.StatusRequestEntityTooLarge, errors.New("docker result is too large"))
		return
	}
	s.dockerQueue.StoreResult(&docker.Response{
		DeviceID:  deviceID,
		RequestID: req.RequestID,
		Status:    req.Status,
		Message:   req.Message,
	})
	// Broadcast updated Docker state to SSE subscribers
	state := s.getDockerState(deviceID)
	s.dockerBroadcaster.Broadcast(deviceID, state)
	w.WriteHeader(http.StatusNoContent)
}

// handleSecurityAudit queues a Lynis audit command for a device.
func (s *Server) handleSecurityAudit(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	// Check for existing pending lynis_audit command
	pending, err := s.store.GetPendingCommands(deviceID)
	if err == nil {
		for _, cmd := range pending {
			if cmd.Type == "lynis_audit" {
				s.writeError(w, http.StatusConflict, errors.New("audit already in progress"))
				return
			}
		}
	}
	command, err := s.store.QueueCommand(deviceID, "lynis_audit", "", "", "", "")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info(logging.CatDevice, logging.EventCmdCreated,
		"lynis audit queued",
		map[string]any{"device_id": deviceID, "command_id": command.ID},
	)
	_ = json.NewEncoder(w).Encode(command)
}

// handleSecurityAudits lists audit history for a device.
func (s *Server) handleSecurityAudits(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	audits, err := s.store.GetSecurityAudits(deviceID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if audits == nil {
		audits = []store.SecurityAudit{}
	}
	_ = json.NewEncoder(w).Encode(audits)
}

// handleSecurityAuditDetail returns a single audit with full report.
func (s *Server) handleSecurityAuditDetail(w http.ResponseWriter, r *http.Request, deviceID, auditID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	audit, err := s.store.GetSecurityAudit(auditID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, errors.New("audit not found"))
		return
	}
	if audit.DeviceID != deviceID {
		s.writeError(w, http.StatusNotFound, errors.New("audit not found for this device"))
		return
	}
	_ = json.NewEncoder(w).Encode(audit)
}

// handleSecurityAuditDelete removes an audit record.
func (s *Server) handleSecurityAuditDelete(w http.ResponseWriter, r *http.Request, deviceID, auditID string) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, err := s.store.GetDevice(deviceID)
	if err != nil || device.OwnerID != s.ownerID(r) {
		s.writeError(w, http.StatusNotFound, errors.New("device not found"))
		return
	}
	if err := s.store.DeleteSecurityAudit(auditID, device.OwnerID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
