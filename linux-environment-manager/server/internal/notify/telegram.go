package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// telegramConfig is the decrypted configuration for the Telegram provider.
type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

var telegramTokenPattern = regexp.MustCompile(`^\d{6,}:[A-Za-z0-9_-]{30,}$`)

type telegramProvider struct{}

func init() { Register(telegramProvider{}) }

func (telegramProvider) Name() string  { return "telegram" }
func (telegramProvider) Label() string { return "Telegram" }
func (telegramProvider) Description() string {
	return "Deliver alerts to a Telegram chat via a bot created with @BotFather."
}

// PublicFields exposes only the chat id; the bot token is write-only.
func (telegramProvider) PublicFields() []string { return []string{"chat_id"} }

func (p telegramProvider) Validate(raw json.RawMessage) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	if cfg.ChatID == "" {
		return errors.New("chat_id is required")
	}
	if len(cfg.ChatID) > 64 || strings.ContainsAny(cfg.ChatID, "\r\n\t ") {
		return errors.New("chat_id is invalid")
	}
	return nil
}

func (telegramProvider) parse(raw json.RawMessage) (telegramConfig, error) {
	var cfg telegramConfig
	if len(raw) == 0 {
		return cfg, errors.New("telegram configuration is empty")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid telegram configuration JSON: %w", err)
	}
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	cfg.ChatID = strings.TrimSpace(cfg.ChatID)
	if !telegramTokenPattern.MatchString(cfg.BotToken) {
		return cfg, errors.New("bot_token does not look like a Telegram bot token")
	}
	return cfg, nil
}

// telegramReply is the subset of the Bot API envelope we need for errors.
type telegramReply struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

func (p telegramProvider) sendMessage(ctx context.Context, client *http.Client, cfg telegramConfig, text string) error {
	// Telegram hard-caps messages at 4096 UTF-8 characters.
	if len(text) > 4000 {
		text = text[:4000]
	}
	body, err := json.Marshal(map[string]string{
		"chat_id":                  cfg.ChatID,
		"text":                     text,
		"disable_web_page_preview": "true",
	})
	if err != nil {
		return fmt.Errorf("telegram marshal body: %w", err)
	}
	endpoint := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return fmt.Errorf("telegram read reply body: %w", err)
	}
	var reply telegramReply
	if err := json.Unmarshal(payload, &reply); err != nil {
		return fmt.Errorf("telegram: unexpected reply format (HTTP %d, body: %s)", resp.StatusCode, string(payload[:min(len(payload), 200)]))
	}
	if !reply.OK {
		return fmt.Errorf("telegram API error (HTTP %d): %s", resp.StatusCode, reply.Description)
	}
	return nil
}

func (p telegramProvider) Send(ctx context.Context, client *http.Client, raw json.RawMessage, e Event) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	return p.sendMessage(ctx, client, cfg, FormatMessage(e))
}

func (p telegramProvider) Test(ctx context.Context, client *http.Client, raw json.RawMessage) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return fmt.Errorf("telegram test: parse config: %w", err)
	}
	return p.sendMessage(ctx, client, cfg, "✅ LEM test notification — your Telegram integration is working.")
}

// DetectChatID calls the Telegram getUpdates endpoint and returns the first
// non-bot chat_id found. The user must have sent at least one message to the
// bot before this will work.
func DetectChatID(ctx context.Context, client *http.Client, botToken string) (string, error) {
	botToken = strings.TrimSpace(botToken)
	if !telegramTokenPattern.MatchString(botToken) {
		return "", fmt.Errorf("bot_token does not look like a Telegram bot token")
	}

	endpoint := "https://api.telegram.org/bot" + botToken + "/getUpdates"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("telegram getUpdates: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram getUpdates HTTP: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("telegram getUpdates read: %w", err)
	}

	var result struct {
		OK          bool `json:"ok"`
		Description string `json:"description"`
		Result      []struct {
			Message *struct {
				Chat struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
				From struct {
					ID       int64  `json:"id"`
					IsBot    bool   `json:"is_bot"`
					Username string `json:"username"`
				} `json:"from"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", fmt.Errorf("telegram getUpdates: unexpected response (HTTP %d)", resp.StatusCode)
	}
	if !result.OK {
		return "", fmt.Errorf("telegram API error: %s", result.Description)
	}

	// Find the first private chat where the sender is not a bot.
	for _, update := range result.Result {
		if update.Message == nil {
			continue
		}
		msg := update.Message
		if msg.From.IsBot {
			continue
		}
		if msg.Chat.Type != "private" {
			continue
		}
		return fmt.Sprintf("%d", msg.Chat.ID), nil
	}

	return "", fmt.Errorf("no chats found — send /start to your bot in Telegram first, then try again")
}
