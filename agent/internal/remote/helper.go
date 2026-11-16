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

	// Host runs interactive sessions. Nil disables them, which is the state of
	// a helper that was not given a session host.
	Host *SessionHost
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
		if h.Host != nil {
			h.Host.CloseAll()
		}
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
	// Attaching waits for a dial and an ssh handshake; a key operation does not.
	// The deadline is extended only for the action that needs it.
	if req.Action == ActionSessionOpen {
		_ = conn.SetDeadline(time.Now().Add(h.sessionTimeout()))
	}
	resp, err := h.dispatch(req)
	if err != nil {
		writeResp(conn, Response{Error: err.Error()})
		return
	}
	writeResp(conn, resp)
}

func (h *Helper) sessionTimeout() time.Duration {
	if h.Timeout > 30*time.Second {
		return h.Timeout
	}
	return 30 * time.Second
}

func (h *Helper) dispatch(req Request) (Response, error) {
	switch req.Action {
	case ActionInject:
		if h.InjectFn != nil {
			return Response{OK: true}, h.InjectFn(req.User, req.Key, req.ID)
		}
		return Response{OK: true}, InjectUser(req.User, req.Key, h.KeyType, h.Options, req.ID, h.TTL)
	case ActionRevoke:
		if h.RevokeFn != nil {
			return Response{OK: true}, h.RevokeFn(req.User, req.ID)
		}
		return Response{OK: true}, RevokeUser(req.User, req.ID)
	case ActionSessionOpen:
		if h.Host == nil {
			return Response{}, errors.New("this helper does not host sessions")
		}
		return Response{OK: true}, h.Host.Open(req.Spec())
	case ActionSessionResize:
		if h.Host == nil {
			return Response{}, errors.New("this helper does not host sessions")
		}
		return Response{OK: true}, h.Host.Resize(req.Ticket, req.Cols, req.Rows)
	case ActionSessionCount:
		if h.Host == nil {
			return Response{OK: true}, nil
		}
		return Response{OK: true, Sessions: h.Host.Count()}, nil
	default:
		return Response{}, errors.New("unknown action")
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
