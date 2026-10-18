package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
)

// In-site alerts do not call any external service: the dispatcher already
// records every event into the inbox table and the dashboard polls it. This
// provider exists so the dashboard can persist the user's in-site
// preferences (enabled, sound) through the same config pipeline as external
// providers.
type inboxConfig struct {
	Sound     string   `json:"sound"`                // "beep" | "chime" | "pulse" | "none"
	Volume    *float64 `json:"volume,omitempty"`     // 0..1, default 0.5
	EventsSet bool     `json:"events_set,omitempty"` // the user has chosen an in-app event list
}

var inboxSounds = []string{"beep", "chime", "pulse", "none"}

type inboxProvider struct{}

func init() { Register(inboxProvider{}) }

func (inboxProvider) Name() string  { return "web_inbox" }
func (inboxProvider) Label() string { return "In-site alerts" }
func (inboxProvider) Description() string {
	return "Show a popup with sound directly in the dashboard. No external account needed."
}

// PublicFields returns every field: the inbox config holds no secrets.
func (inboxProvider) PublicFields() []string { return []string{"sound", "volume", "events_set"} }

func (p inboxProvider) parse(raw json.RawMessage) (inboxConfig, error) {
	cfg := inboxConfig{Sound: "beep"}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid inbox configuration JSON: %w", err)
	}
	if cfg.Sound == "" {
		cfg.Sound = "beep"
	}
	if cfg.Volume == nil {
		volume := 0.5
		cfg.Volume = &volume
	}
	return cfg, nil
}

func (p inboxProvider) Validate(raw json.RawMessage) error {
	cfg, err := p.parse(raw)
	if err != nil {
		return err
	}
	if !slices.Contains(inboxSounds, cfg.Sound) {
		return errors.New("sound must be one of: beep, chime, pulse, none")
	}
	if cfg.Volume == nil || *cfg.Volume < 0 || *cfg.Volume > 1 {
		return errors.New("volume must be between 0 and 1")
	}
	return nil
}

// Send is a deliberate no-op: the inbox row is written by the dispatcher
// before provider fan-out.
func (inboxProvider) Send(context.Context, *http.Client, json.RawMessage, Event) error {
	return nil
}

// Test is a deliberate no-op; the dashboard plays the selected sound locally.
func (inboxProvider) Test(context.Context, *http.Client, json.RawMessage) error {
	return nil
}
