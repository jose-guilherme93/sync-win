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
	"net/http"
	"net/url"
	"strings"
)

// webhookConfig is the decrypted configuration for the generic webhook
// provider. Secret is optional; when set it signs the body with HMAC-SHA256
// in the X-LEM-Signature header so the receiver can verify authenticity.
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
	if len(cfg.Secret) > 256 {
		return cfg, errors.New("webhook secret is too long")
	}
	return cfg, nil
}

func (p webhookProvider) Validate(raw json.RawMessage) error {
	_, err := p.parse(raw)
	return err
}

// webhookPayload is the canonical JSON body delivered to endpoints.
type webhookPayload struct {
	Event      Event  `json:"event"`
	Text       string `json:"text"`
	DeliveredBy string `json:"delivered_by"`
}

func (p webhookProvider) post(ctx context.Context, client *http.Client, cfg webhookConfig, payload webhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lem-server/1 (notification webhook)")
	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(body)
		req.Header.Set("X-LEM-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := client.Do(req)
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
	return p.post(ctx, client, cfg, webhookPayload{Event: e, Text: FormatMessage(e), DeliveredBy: "lem"})
}

func (p webhookProvider) Test(ctx context.Context, client *http.Client, raw json.RawMessage) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	return p.post(ctx, client, cfg, webhookPayload{
		Event:       Event{Type: "test", Message: "LEM test notification"},
		Text:        "✅ LEM test notification — your webhook integration is working.",
		DeliveredBy: "lem",
	})
}
