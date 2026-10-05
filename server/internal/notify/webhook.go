package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// webhookConfig is the decrypted configuration for the generic webhook
// provider. Secret is optional; when set it signs the body with HMAC-SHA256
// in the X-SYNCWIN-Signature header so the receiver can verify authenticity.
type webhookConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

type webhookProvider struct{}

func init() { Register(webhookProvider{}) }

func (webhookProvider) Name() string  { return "webhook" }
func (webhookProvider) Label() string { return "Webhook" }
func (webhookProvider) Description() string {
	return "POST every event as JSON to any HTTP(S) endpoint (Discord, Slack, n8n, Home Assistant, ...)."
}

// PublicFields exposes the URL; the signing secret is write-only.
func (webhookProvider) PublicFields() []string { return []string{"url"} }

func (p webhookProvider) parse(raw json.RawMessage) (webhookConfig, error) {
	var cfg webhookConfig
	if len(raw) == 0 {
		return cfg, errors.New("webhook configuration is empty")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid webhook configuration JSON: %w", err)
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	if len(cfg.URL) > 2048 {
		return cfg, errors.New("webhook URL is too long")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" {
		return cfg, errors.New("webhook URL is invalid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return cfg, errors.New("webhook URL must use http or https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "localhost.localdomain" {
		return cfg, errors.New("webhook URL resolves to a private address")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return cfg, errors.New("webhook URL resolves to a private address")
	}
	if len(cfg.Secret) > 256 {
		return cfg, errors.New("webhook secret is too long")
	}
	return cfg, nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

// lookupIPAddr is a seam for tests; production uses the default resolver.
var lookupIPAddr = net.DefaultResolver.LookupIPAddr

// dialWebhook is a seam for tests; production dials the pinned IP directly.
var dialWebhook = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext

// pinnedWebhookClient resolves the target once, rejects any private address,
// and returns a client whose dialer always connects to that exact validated IP.
//
// Resolving and dialing separately (the previous behaviour) is a TOCTOU
// DNS-rebinding hole: a hostname can return a public address to the validation
// lookup and a private one to the dial lookup. Pinning the IP closes it, while
// TLS still validates the certificate against the original hostname.
func pinnedWebhookClient(ctx context.Context, base *http.Client, rawURL string) (*http.Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("webhook URL is invalid")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "localhost.localdomain" {
		return nil, errors.New("webhook URL resolves to a private address")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	pinnedIP := net.ParseIP(host)
	if pinnedIP == nil {
		addrs, err := lookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve webhook host: %w", err)
		}
		if len(addrs) == 0 {
			return nil, errors.New("webhook host has no addresses")
		}
		for _, addr := range addrs {
			if isPrivateIP(addr.IP) {
				return nil, errors.New("webhook URL resolves to a private address")
			}
			if pinnedIP == nil {
				pinnedIP = addr.IP
			}
		}
	} else if isPrivateIP(pinnedIP) {
		return nil, errors.New("webhook URL resolves to a private address")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	pinned := pinnedIP.String()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialWebhook(ctx, network, net.JoinHostPort(pinned, port))
	}

	client := *base
	client.Transport = transport
	// Redirects are disabled: each one would target a host that was never
	// validated against the private-address filter.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("webhook redirects are disabled")
	}
	return &client, nil
}

func (p webhookProvider) Validate(raw json.RawMessage) error {
	_, err := p.parse(raw)
	return err
}

// webhookPayload is the canonical JSON body delivered to endpoints.
type webhookPayload struct {
	Event       Event  `json:"event"`
	Text        string `json:"text"`
	DeliveredBy string `json:"delivered_by"`
}

func (p webhookProvider) post(ctx context.Context, client *http.Client, cfg webhookConfig, payload webhookPayload) error {
	if client == nil {
		client = http.DefaultClient
	}
	pinnedClient, err := pinnedWebhookClient(ctx, client, cfg.URL)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sync-win-server/1 (notification webhook)")
	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(body)
		req.Header.Set("X-SYNCWIN-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := pinnedClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook send: %w", err)
	}
	defer resp.Body.Close()
	// Drain a small prefix so keep-alive connections can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p webhookProvider) Send(ctx context.Context, client *http.Client, raw json.RawMessage, e Event) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	return p.post(ctx, client, cfg, webhookPayload{Event: e, Text: FormatMessage(e), DeliveredBy: "sync-win"})
}

func (p webhookProvider) Test(ctx context.Context, client *http.Client, raw json.RawMessage) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	return p.post(ctx, client, cfg, webhookPayload{
		Event:       Event{Type: "test", Message: "SyncWin test notification"},
		Text:        "✅ SyncWin test notification — your webhook integration is working.",
		DeliveredBy: "sync-win",
	})
}
