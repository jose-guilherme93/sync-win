package app

import (
	"os"
	"strings"
)

// featureFlags contains deliberately conservative kill switches for
// capabilities that are not safe to expose until their security controls are
// fully deployed. Flags are opt-in; production therefore fails closed.
type featureFlags struct {
	EnableFingerprintReconnect bool
	EnableLegacyInstall        bool
	EnableRemoteMutations      bool
	EnableDockerMutations      bool
	EnableRegistration         bool
	// EnableRemoteAccess exposes the interactive-tunnel endpoints. Off by
	// default: the broker can open a shell on a device, so it is opt-in on the
	// server as well as on the device (policy.json allow_remote_access).
	EnableRemoteAccess bool
	// EnableDevLogin accepts SYNCWIN_DEV_TOKEN as a dashboard sign-in for
	// DevEmail. Only possible when SYNCWIN_ENV=development, so a production
	// server cannot be entered with the development token.
	EnableDevLogin bool
	DevToken       string
	DevEmail       string
}

func loadFeatureFlags() featureFlags {
	devToken := strings.TrimSpace(os.Getenv("SYNCWIN_DEV_TOKEN"))
	devEmail := strings.TrimSpace(os.Getenv("SYNCWIN_ADMIN_EMAIL"))
	if devEmail == "" {
		devEmail = "demo@sync-win.local"
	}
	return featureFlags{
		EnableFingerprintReconnect: envBool("SYNCWIN_ENABLE_FINGERPRINT_RECONNECT", false),
		EnableLegacyInstall:        envBool("SYNCWIN_ENABLE_LEGACY_INSTALL", false),
		EnableRemoteMutations:      envBool("SYNCWIN_ENABLE_REMOTE_MUTATIONS", false),
		EnableDockerMutations:      envBool("SYNCWIN_ENABLE_DOCKER_MUTATIONS", false),
		EnableRegistration:         envBool("SYNCWIN_ENABLE_REGISTRATION", false),
		EnableRemoteAccess:         envBool("SYNCWIN_ENABLE_REMOTE_ACCESS", false),
		EnableDevLogin:             strings.TrimSpace(os.Getenv("SYNCWIN_ENV")) == "development" && devToken != "",
		DevToken:                   devToken,
		DevEmail:                   devEmail,
	}
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
