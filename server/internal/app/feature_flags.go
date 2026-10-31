package app

import (
	"fmt"
	"os"
	"strings"
)

// minAccessTokenLen is the shortest dashboard token SyncWin accepts. The token
// is the only credential the dashboard has, so a short one is a typo away from
// being guessed; 32 characters is also what `openssl rand -hex 16` produces.
const minAccessTokenLen = 32

// featureFlags contains deliberately conservative kill switches for
// capabilities that are not safe to expose until their security controls are
// fully deployed. Flags are opt-in; production therefore fails closed.
type featureFlags struct {
	EnableFingerprintReconnect bool
	EnableLegacyInstall        bool
	EnableRemoteMutations      bool
	EnableDockerMutations      bool
	// EnableRemoteAccess exposes the interactive-tunnel endpoints. Off by
	// default: the broker can open a shell on a device, so it is opt-in on the
	// server as well as on the device (policy.json allow_remote_access).
	EnableRemoteAccess bool
	// AccessToken is the single dashboard credential, read from
	// SYNCWIN_ACCESS_TOKEN. It is never accepted from a stored copy other than
	// the environment, and it is never written to the database.
	AccessToken string
	// AccessTokenProblem names what is wrong with the token when sign-in is
	// unavailable. Empty means the token is usable.
	AccessTokenProblem string
}

// AccessTokenUsable reports whether dashboard sign-in can be served.
func (f featureFlags) AccessTokenUsable() bool { return f.AccessTokenProblem == "" }

func loadFeatureFlags() featureFlags {
	token := strings.TrimSpace(os.Getenv("SYNCWIN_ACCESS_TOKEN"))
	problem := ""
	switch {
	case token == "":
		problem = "SYNCWIN_ACCESS_TOKEN is not set; the dashboard cannot be opened until it is"
	case len(token) < minAccessTokenLen:
		problem = fmt.Sprintf("SYNCWIN_ACCESS_TOKEN must be at least %d characters; the dashboard stays locked until it is", minAccessTokenLen)
	}
	return featureFlags{
		EnableFingerprintReconnect: envBool("SYNCWIN_ENABLE_FINGERPRINT_RECONNECT", false),
		EnableLegacyInstall:        envBool("SYNCWIN_ENABLE_LEGACY_INSTALL", false),
		EnableRemoteMutations:      envBool("SYNCWIN_ENABLE_REMOTE_MUTATIONS", false),
		EnableDockerMutations:      envBool("SYNCWIN_ENABLE_DOCKER_MUTATIONS", false),
		EnableRemoteAccess:         envBool("SYNCWIN_ENABLE_REMOTE_ACCESS", false),
		AccessToken:                token,
		AccessTokenProblem:         problem,
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
