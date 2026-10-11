package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/coder/websocket"
)

// NewAgentLink adapts an accepted WebSocket to an AgentLink. coder/websocket
// permits only one concurrent writer, and the hub may Send from several session
// goroutines, so writes are serialized.
func NewAgentLink(conn *websocket.Conn) AgentLink {
	return &wsAgentLink{conn: conn}
}

type wsAgentLink struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (l *wsAgentLink) Send(msg AgentMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.conn.Write(context.Background(), websocket.MessageText, data)
}

func (l *wsAgentLink) Close() error {
	return l.conn.Close(websocket.StatusNormalClosure, "replaced by a newer connection")
}

// NewSessionConn adapts an accepted WebSocket to a byte pipe. Each WebSocket
// message is one chunk; Read stitches messages together.
func NewSessionConn(conn *websocket.Conn) SessionConn {
	return &wsSessionConn{conn: conn, done: make(chan struct{})}
}

type wsSessionConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
	rd   io.Reader
	once sync.Once
	done chan struct{}
}

func (c *wsSessionConn) Read(p []byte) (int, error) {
	for {
		if c.rd == nil {
			_, data, err := c.conn.Read(context.Background())
			if err != nil {
				return 0, err
			}
			c.rd = bytes.NewReader(data)
		}
		n, err := c.rd.Read(p)
		if err == io.EOF {
			c.rd = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *wsSessionConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.Write(context.Background(), websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsSessionConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.conn.Close(websocket.StatusNormalClosure, "session closed")
}

// Done is closed when Close is called. It lets the agent-side handler block
// until the browser relay finishes with the pipe, without touching Read.
func (c *wsSessionConn) Done() <-chan struct{} {
	return c.done
}
