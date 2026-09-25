package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName       = "lem_session"
	csrfCookieName          = "lem_csrf"
	csrfHeaderName          = "X-LEM-CSRF"
	defaultSessionCookieAge = 30 * 24 * time.Hour
)

func deviceTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isDeviceTokenDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func csrfCookieMatches(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	return header != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

func csrfExemptPath(path string) bool {
	return path == "/api/auth/login" || path == "/api/auth/register" || strings.HasPrefix(path, "/api/agent/")
}

func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfSafeMethod(r.Method) || csrfExemptPath(r.URL.Path) || r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := r.Cookie(sessionCookieName); err != nil || !csrfCookieMatches(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "CSRF validation failed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sessionToken(r *http.Request) string {
	if token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); token != "" {
		return token
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func (s *Server) sessionOwner(r *http.Request) (string, bool) {
	token := s.sessionToken(r)
	if token == "" {
		return "", false
	}
	return s.store.SessionOwner(token)
}

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	ownerID, ok := s.sessionOwner(r)
	if !ok || ownerID == "" {
		s.writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return "", false
	}
	if !csrfSafeMethod(r.Method) && r.Header.Get("Authorization") == "" && !csrfCookieMatches(r) {
		s.writeError(w, http.StatusForbidden, errors.New("CSRF validation failed"))
		return "", false
	}
	return ownerID, true
}

func newCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func setCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(defaultSessionCookieAge / time.Second),
	})
}

func clearCSRFCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		Secure:   r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(defaultSessionCookieAge / time.Second),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	clearCSRFCookie(w, r)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
