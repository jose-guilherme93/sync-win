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
}

func loadFeatureFlags() featureFlags {
	return featureFlags{
		EnableFingerprintReconnect: envBool("LEM_ENABLE_FINGERPRINT_RECONNECT", false),
		EnableLegacyInstall:        envBool("LEM_ENABLE_LEGACY_INSTALL", false),
		EnableRemoteMutations:      envBool("LEM_ENABLE_REMOTE_MUTATIONS", false),
		EnableDockerMutations:      envBool("LEM_ENABLE_DOCKER_MUTATIONS", false),
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
