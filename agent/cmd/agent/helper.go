package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strconv"
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

	h := &remote.Helper{
		SocketPath: *socket,
		AllowedUID: uint32(uid),
		AllowedGID: uint32(gid),
		KeyType:    keyType,
		Options:    options,
		TTL:        time.Duration(syncwinContract.RemoteIdleSeconds()) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("sync-win remote helper listening on %s (peer uid %d)", *socket, uid)
	return h.Serve(ctx)
}
