package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Helper is the root-side server. It accepts one request per connection over a
// Unix socket and performs exactly one key operation.
type Helper struct {
	SocketPath string
	// AllowedUID is the only peer the helper will answer (the agent's service
	// user). AllowedGID owns the socket so that the agent may connect at all.
	AllowedUID uint32
	AllowedGID uint32
	KeyType    string
	Options    string
	TTL        time.Duration
	Timeout    time.Duration

	// InjectFn and RevokeFn are seams for tests. When nil the real root-side
	// operations run.
	InjectFn func(user, key, id string) error
	RevokeFn func(user, id string) error
}

// Serve listens on the helper socket until the context is canceled.
func (h *Helper) Serve(ctx context.Context) error {
	if h.SocketPath == "" {
		return errors.New("helper socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(h.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(h.SocketPath)
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", h.SocketPath)
	if err != nil {
		return err
	}
	// The socket is the access control: group-readable by the agent, and every
	// accepted peer is checked against AllowedUID as well. Only the group is
	// changed: a non-root test (or a check run) cannot chown to root, and the
	// owner already is whoever runs the helper.
	if err := os.Chown(h.SocketPath, -1, int(h.AllowedGID)); err != nil {
		ln.Close()
		return err
	}
	if err := os.Chmod(h.SocketPath, 0o660); err != nil {
		ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go h.handle(conn)
	}
}

func (h *Helper) handle(conn net.Conn) {
	defer conn.Close()
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if uid, err := peerUID(conn); err != nil || uid != h.AllowedUID {
		writeResp(conn, Response{Error: "peer is not authorized"})
		return
	}

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResp(conn, Response{Error: "malformed request"})
		return
	}
	if err := req.Validate(); err != nil {
		writeResp(conn, Response{Error: err.Error()})
		return
	}
	if err := h.dispatch(req); err != nil {
		writeResp(conn, Response{Error: err.Error()})
		return
	}
	writeResp(conn, Response{OK: true})
}

func (h *Helper) dispatch(req Request) error {
	switch req.Action {
	case ActionInject:
		if h.InjectFn != nil {
			return h.InjectFn(req.User, req.Key, req.ID)
		}
		return InjectUser(req.User, req.Key, h.KeyType, h.Options, req.ID, h.TTL)
	case ActionRevoke:
		if h.RevokeFn != nil {
			return h.RevokeFn(req.User, req.ID)
		}
		return RevokeUser(req.User, req.ID)
	default:
		return errors.New("unknown action")
	}
}

func writeResp(conn net.Conn, resp Response) {
	_ = json.NewEncoder(conn).Encode(resp)
}

// peerUID returns the uid of the process on the other end of a Unix socket.
func peerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return cred.Uid, nil
}
