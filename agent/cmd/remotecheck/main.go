// Command remotecheck drives one real remote-access session end to end.
//
// It exists because the failure it checks for could not be seen by a unit test:
// an agent update restarted the agent that hosted the operator's terminal, the
// session died with it, and a machine whose only way in was that terminal could
// not be reached again. A unit test can prove the helper hosts a session; only
// the real stack can prove the session keeps working while the agent is
// replaced underneath it.
//
// It signs in like the dashboard does, requests a ticket, attaches as the
// browser would, proves the shell answers, runs a command that restarts the
// agent, and proves the same shell answers again afterwards.
//
// Usage (against the dev stack with the full test device running):
//
//	go run ./cmd/remotecheck \
//	  -server http://localhost:8088 \
//	  -token dev-access-token-0123456789abcdef \
//	  -hostname ubuntu-full-1 \
//	  -user devuser \
//	  -restart 'docker exec sync-win-test-agent-full systemctl restart sync-win-agent'
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type config struct {
	server   string
	token    string
	device   string
	hostname string
	user     string
	restart  string
	wait     time.Duration
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.server, "server", "http://localhost:8088", "server base URL")
	flag.StringVar(&cfg.token, "token", "dev-access-token-0123456789abcdef", "dashboard access token")
	flag.StringVar(&cfg.device, "device", "", "device id (resolved from -hostname when empty)")
	flag.StringVar(&cfg.hostname, "hostname", "ubuntu-full-1", "device hostname to resolve")
	flag.StringVar(&cfg.user, "user", "devuser", "account the session logs in as")
	flag.StringVar(&cfg.restart, "restart", "", "shell command that restarts the agent during the session")
	flag.DurationVar(&cfg.wait, "wait", 20*time.Second, "how long to wait for a command's output")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASS: the session survived the agent restart")
}

func run(cfg config) error {
	client, err := signIn(cfg.server, cfg.token)
	if err != nil {
		return err
	}
	deviceID := cfg.device
	if deviceID == "" {
		deviceID, err = findDevice(client, cfg.server, cfg.hostname)
		if err != nil {
			return err
		}
	}
	fmt.Printf("device: %s\n", deviceID)

	var pidBefore string
	if cfg.restart != "" {
		pidBefore, err = agentPID(cfg.restart)
		if err != nil {
			return fmt.Errorf("read the agent pid before the session: %w", err)
		}
		fmt.Printf("agent pid before: %s\n", pidBefore)
	}

	session, err := attach(client, cfg.server, deviceID, cfg.user)
	if err != nil {
		return err
	}
	defer session.close()

	first := fmt.Sprintf("SYNCWIN-BEFORE-%d", time.Now().UnixNano())
	if expectErr := session.expect(first, cfg.wait); expectErr != nil {
		return fmt.Errorf("the shell did not answer before the restart: %w", expectErr)
	}
	fmt.Println("ok: the shell answers before the agent restart")

	if cfg.restart == "" {
		fmt.Println("skipped: no -restart command, so nothing was replaced")
		return nil
	}
	if restartErr := runCommand(cfg.restart); restartErr != nil {
		return fmt.Errorf("restart the agent: %w", restartErr)
	}
	pidAfter, err := agentPID(cfg.restart)
	if err != nil {
		return fmt.Errorf("read the agent pid after the session: %w", err)
	}
	fmt.Printf("agent pid after:  %s\n", pidAfter)
	if pidBefore == pidAfter {
		return fmt.Errorf("the agent pid did not change (%s); the restart did not happen", pidAfter)
	}

	// The point of hosting the terminal in the helper: this same socket, opened
	// before the restart, is still a live shell.
	second := fmt.Sprintf("SYNCWIN-AFTER-%d", time.Now().UnixNano())
	if err := session.expect(second, cfg.wait); err != nil {
		return fmt.Errorf("the session did not survive the agent restart: %w", err)
	}
	fmt.Println("ok: the same session answers after the agent restarted")
	return nil
}

// --- dashboard API ----------------------------------------------------------

// newRequest builds an API request with a context, so no call can hang forever.
func newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	if body == nil {
		return http.NewRequestWithContext(ctx, method, url, nil)
	}
	return http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
}

func signIn(base, token string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	body, _ := json.Marshal(map[string]string{"token": token})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := newRequest(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/auth/login", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sign in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sign in returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return client, nil
}

func findDevice(client *http.Client, base, hostname string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := newRequest(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/devices", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list devices: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read devices: %w", err)
	}
	type device struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
	}
	var devices []device
	if err := json.Unmarshal(raw, &devices); err != nil {
		// The endpoint has answered both as an array and as {"devices": [...]}.
		var wrapped struct {
			Devices []device `json:"devices"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return "", fmt.Errorf("decode devices: %w", err)
		}
		devices = wrapped.Devices
	}
	for _, d := range devices {
		if d.Hostname == hostname {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("no device named %q", hostname)
}

func csrfToken(client *http.Client, base string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, cookie := range client.Jar.Cookies(parsed) {
		if cookie.Name == "syncwin_csrf" {
			return cookie.Value
		}
	}
	return ""
}

func attach(client *http.Client, base, deviceID, user string) (*terminal, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"user": user, "cols": 120, "rows": 30})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := newRequest(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/api/devices/"+url.PathEscape(deviceID)+"/remote-access/session", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SYNCWIN-CSRF", csrfToken(client, base))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request a session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("session request returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var issued struct {
		Ticket string `json:"ticket"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&issued); decodeErr != nil {
		return nil, fmt.Errorf("decode the ticket: %w", decodeErr)
	}
	if issued.Ticket == "" {
		return nil, errors.New("the server returned no ticket")
	}

	// The cookie jar only answers for http/https URLs, so collect the cookies
	// before the scheme becomes ws/wss.
	cookieURL := *parsed
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/devices/" + url.PathEscape(deviceID) + "/terminal"
	query := url.Values{"ticket": {issued.Ticket}}
	parsed.RawQuery = query.Encode()

	// One Cookie header: several header lines are legal but not every server
	// path reads them, and an attach that silently loses the session cookie
	// fails with a bare 401.
	var pairs []string
	for _, cookie := range client.Jar.Cookies(&cookieURL) {
		pairs = append(pairs, cookie.Name+"="+cookie.Value)
	}
	if len(pairs) == 0 {
		return nil, errors.New("no dashboard cookie to attach with")
	}
	header := http.Header{}
	header.Set("Cookie", strings.Join(pairs, "; "))
	conn, wsResp, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if wsResp != nil && wsResp.Body != nil {
		_ = wsResp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("attach the session: %w", err)
	}
	t := &terminal{conn: conn, output: &syncBuffer{}}
	go t.pump()
	return t, nil
}

type terminal struct {
	conn   *websocket.Conn
	output *syncBuffer
}

func (t *terminal) pump() {
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			return
		}
		_, _ = t.output.Write(data)
	}
}

// expect sends a command that prints a unique marker and waits for that marker
// to come back from the remote shell.
func (t *terminal) expect(marker string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := fmt.Sprintf("printf '%s\\n'\n", marker)
	if err := t.conn.Write(ctx, websocket.MessageBinary, []byte(command)); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if strings.Contains(t.output.String(), marker) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("no %q in the terminal output; last output: %q", marker, tail(t.output.String(), 300))
}

func (t *terminal) close() {
	_ = t.conn.Close(websocket.StatusNormalClosure, "")
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// --- container helpers ------------------------------------------------------

// agentPID reports the agent's main pid, reading it through the same command
// that restarts it (a `docker exec ... systemctl ...`), so a test and the
// restart share one entry point.
func agentPID(command string) (string, error) {
	if command == "" {
		return "", errors.New("no restart command")
	}
	show := strings.Replace(command, "systemctl restart sync-win-agent", "systemctl show -p MainPID --value sync-win-agent", 1)
	if show == command {
		return "", errors.New("the restart command must contain `systemctl restart sync-win-agent`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", show).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func runCommand(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
