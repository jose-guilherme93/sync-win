package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sync-win/agent/internal/remote"
)

// cmdHelper runs the privileged half of remote access. It is a separate,
// root-owned systemd service; it listens on a Unix socket and only ever
// injects or revokes a single restricted key line for one named user.
func cmdHelper(args []string) error {
	fs := flag.NewFlagSet("helper", flag.ExitOnError)
	socket := fs.String("socket", syncwinContract.RemoteAccess.HelperSocketPath, "helper unix socket path")
	userName := fs.String("user", "sync-win", "only this user may call the helper")
	groupName := fs.String("group", "sync-win", "group that owns the socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socket == "" {
		*socket = "/run/sync-win/helper.sock"
	}

	u, err := user.Lookup(*userName)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", *userName, err)
	}
	uid, uidErr := strconv.Atoi(u.Uid)
	if uidErr != nil {
		return fmt.Errorf("user %q has a non-numeric uid", *userName)
	}
	g, err := user.LookupGroup(*groupName)
	if err != nil {
		return fmt.Errorf("lookup group %q: %w", *groupName, err)
	}
	gid, gidErr := strconv.Atoi(g.Gid)
	if gidErr != nil {
		return fmt.Errorf("group %q has a non-numeric gid", *groupName)
	}

	keyType := syncwinContract.RemoteAccess.KeyType
	if keyType == "" {
		keyType = "ssh-ed25519"
	}
	options := syncwinContract.RemoteAccess.AuthorizedKeysOption
	if options == "" {
		options = `restrict,pty,from="127.0.0.1"`
	}

	ttl := time.Duration(syncwinContract.RemoteIdleSeconds()) * time.Second
	h := &remote.Helper{
		SocketPath: *socket,
		AllowedUID: uint32(uid),
		AllowedGID: uint32(gid),
		KeyType:    keyType,
		Options:    options,
		TTL:        ttl,
		// Sessions are hosted here, not in the agent: an update restarts the
		// agent, and a terminal it owns would die with it.
		Host: &remote.SessionHost{
			MaxSessions: syncwinContract.RemoteMaxSessions(),
			Idle:        ttl,
			SSHHost:     syncwinContract.RemoteAccess.SSHHost,
			SSHPort:     syncwinContract.RemoteAccess.SSHPort,
			ServerURL:   helperServerURL,
			Inject: func(user, key, id string) error {
				return remote.InjectUser(user, key, keyType, options, id, ttl)
			},
			Revoke: remote.RevokeUser,
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("sync-win remote helper listening on %s (peer uid %d)", *socket, uid)
	return h.Serve(ctx)
}

// execStartServerPattern finds the --server argument of the agent's ExecStart.
var execStartServerPattern = regexp.MustCompile(`--server[= ]+([^\s"']+)`)

// helperServerURL resolves where a hosted session attaches.
//
// This is the reason the session lives in the helper: the destination must not
// be a value the unprivileged agent supplies, or a compromised agent could use
// the root helper to open connections to a host of its choosing. The URL comes
// from root-owned configuration -- the environment of the helper unit, or the
// --server the agent itself was installed with.
func helperServerURL() (string, error) {
	if value := strings.TrimSpace(os.Getenv("SYNCWIN_SERVER")); value != "" {
		return value, nil
	}
	unit, err := os.ReadFile(agentUnitPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s to find the server URL: %w", agentUnitPath, err)
	}
	match := execStartServerPattern.FindSubmatch(unit)
	if len(match) < 2 {
		return "", fmt.Errorf("%s has no --server argument", agentUnitPath)
	}
	return strings.TrimSpace(string(match[1])), nil
}
