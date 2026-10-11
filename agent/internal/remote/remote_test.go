package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(len("ssh-ed25519")))
	buf.WriteString("ssh-ed25519")
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(key)))
	buf.Write(key)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(buf.Bytes()) + " test@host"
}

func TestComposeAuthorizedKeyLine(t *testing.T) {
	key := testKey(t)
	line, err := composeAuthorizedKeyLine("ssh-ed25519", key, `restrict,pty,from="127.0.0.1"`, "abc123", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, `restrict,pty,from="127.0.0.1",expiry-time="`) {
		t.Fatalf("line lost its restriction prefix: %q", line)
	}
	if !strings.HasSuffix(line, "syncwin:abc123") {
		t.Fatalf("line lost its marker: %q", line)
	}
	if strings.Contains(line, "test@host") {
		t.Fatalf("key comment must be dropped: %q", line)
	}
}

func TestComposeRejectsHostileKey(t *testing.T) {
	if _, err := composeAuthorizedKeyLine("ssh-ed25519", "ssh-rsa AAAA", "", "id", time.Now()); err == nil {
		t.Fatal("wrong key type must be rejected")
	}
	if _, err := composeAuthorizedKeyLine("ssh-ed25519", "ssh-ed25519 AAAA\ncommand=\"x\" ssh-ed25519 AAAA", "", "id", time.Now()); err == nil {
		t.Fatal("newline injection must be rejected")
	}
	if _, err := composeAuthorizedKeyLine("ssh-ed25519", "ssh-ed25519 not-base64!!", "", "id", time.Now()); err == nil {
		t.Fatal("invalid base64 must be rejected")
	}
}

func TestInjectAndRevokeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	uid, gid := os.Getuid(), os.Getgid()
	key := testKey(t)
	line, err := composeAuthorizedKeyLine("ssh-ed25519", key, "restrict", "s1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// Seed an unrelated line that must survive.
	if err := os.WriteFile(path, []byte("ssh-ed25519 AAAAoriginal test@old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InjectFile(path, line, "s1", uid, gid); err != nil {
		t.Fatal(err)
	}
	// Injecting the same id twice must not duplicate the line.
	if err := InjectFile(path, line, "s1", uid, gid); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Count(string(got), "syncwin:s1") != 1 {
		t.Fatalf("marker count = %d, want 1:\n%s", strings.Count(string(got), "syncwin:s1"), got)
	}
	if !strings.Contains(string(got), "original test@old") {
		t.Fatal("unrelated line was removed")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	if err := RevokeFile(path, "s1", uid, gid); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "syncwin:s1") {
		t.Fatalf("marker survived revoke:\n%s", got)
	}
	if !strings.Contains(string(got), "original test@old") {
		t.Fatal("revoke removed an unrelated line")
	}
}

func TestRequestValidate(t *testing.T) {
	if err := (Request{Action: ActionInject, User: "alice", ID: strings.Repeat("a", 16), Key: "ssh-ed25519 AAAA"}).Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := (Request{Action: ActionInject, User: "root", ID: "short", Key: "x"}).Validate(); err == nil {
		t.Fatal("short id must be rejected")
	}
	if err := (Request{Action: "nope", User: "alice", ID: strings.Repeat("a", 16)}).Validate(); err == nil {
		t.Fatal("unknown action must be rejected")
	}
	if err := (Request{Action: ActionInject, User: "bad name", ID: strings.Repeat("a", 16), Key: "x"}).Validate(); err == nil {
		t.Fatal("invalid user must be rejected")
	}
}

func TestHelperServeInjectsAndRevokes(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	var mu sync.Mutex
	var injected, revoked []string
	h := &Helper{
		SocketPath: sock,
		AllowedUID: uint32(os.Getuid()),
		AllowedGID: uint32(os.Getgid()),
		InjectFn: func(user, key, id string) error {
			mu.Lock()
			defer mu.Unlock()
			injected = append(injected, user+"/"+id)
			return nil
		},
		RevokeFn: func(user, id string) error {
			mu.Lock()
			defer mu.Unlock()
			revoked = append(revoked, user+"/"+id)
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Serve(ctx) }()

	waitForSocket(t, sock)

	id := strings.Repeat("a", 16)
	if err := Inject(sock, "alice", testKey(t), id); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := Revoke(sock, "alice", id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	mu.Lock()
	gotInjected, gotRevoked := len(injected), len(revoked)
	mu.Unlock()
	if gotInjected != 1 || gotRevoked != 1 {
		t.Fatalf("injected=%d revoked=%d", gotInjected, gotRevoked)
	}

	// An invalid request is refused, not executed.
	if err := Inject(sock, "bad name", "x", id); err == nil {
		t.Fatal("invalid request must be refused")
	}
}

func TestHelperRejectsOtherUID(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	h := &Helper{
		SocketPath: sock,
		AllowedUID: uint32(os.Getuid()) + 1, // nobody has this uid in the test
		AllowedGID: uint32(os.Getgid()),
		InjectFn:   func(user, key, id string) error { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Serve(ctx) }()
	waitForSocket(t, sock)

	if err := Inject(sock, "alice", testKey(t), strings.Repeat("a", 16)); err == nil {
		t.Fatal("unexpected uid must be rejected")
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket %s never appeared", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
