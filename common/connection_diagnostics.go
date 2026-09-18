package common

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type connectionDiagnosticsKey struct{}

// ConnectionIOFailure records socket failures without addresses or payloads.
// net/http cancels request contexts on both peer errors and local write errors;
// retaining the original I/O failure lets relay logs distinguish the two.
type ConnectionIOFailure struct {
	At       time.Time
	Kind     string
	Error    string
	Deadline time.Time
}

type ConnectionDiagnostics struct {
	ReadFailure  ConnectionIOFailure
	WriteFailure ConnectionIOFailure
	ClosedAt     time.Time
}

type diagnosticConn struct {
	net.Conn
	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
	diagnostics   ConnectionDiagnostics
}

type diagnosticListener struct{ net.Listener }

func WithConnectionDiagnostics(listener net.Listener) net.Listener {
	return &diagnosticListener{Listener: listener}
}

func (l *diagnosticListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &diagnosticConn{Conn: conn}, nil
}

func ConnectionDiagnosticsContext(ctx context.Context, conn net.Conn) context.Context {
	if tracked, ok := conn.(*diagnosticConn); ok {
		return context.WithValue(ctx, connectionDiagnosticsKey{}, tracked)
	}
	return ctx
}

func GetConnectionDiagnostics(ctx context.Context) (ConnectionDiagnostics, bool) {
	tracked, ok := ctx.Value(connectionDiagnosticsKey{}).(*diagnosticConn)
	if !ok {
		return ConnectionDiagnostics{}, false
	}
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	return tracked.diagnostics, true
}

func (c *diagnosticConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		c.mu.Lock()
		c.diagnostics.ReadFailure = connectionIOFailure(err, c.readDeadline)
		c.mu.Unlock()
	}
	return n, err
}

func (c *diagnosticConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		c.mu.Lock()
		c.diagnostics.WriteFailure = connectionIOFailure(err, c.writeDeadline)
		c.mu.Unlock()
	}
	return n, err
}

func connectionIOFailure(err error, deadline time.Time) ConnectionIOFailure {
	kind := "io_error"
	var netErr net.Error
	switch {
	case errors.Is(err, io.EOF):
		kind = "peer_eof"
	case errors.Is(err, net.ErrClosed):
		kind = "connection_closed"
	case errors.As(err, &netErr) && netErr.Timeout():
		kind = "timeout"
	}
	// net.OpError includes endpoint addresses. Retain the underlying reason only.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		err = opErr.Err
	}
	return ConnectionIOFailure{At: time.Now(), Kind: kind, Error: err.Error(), Deadline: deadline}
}

func (c *diagnosticConn) SetReadDeadline(deadline time.Time) error {
	err := c.Conn.SetReadDeadline(deadline)
	if err == nil {
		c.mu.Lock()
		c.readDeadline = deadline
		c.mu.Unlock()
	}
	return err
}

func (c *diagnosticConn) SetWriteDeadline(deadline time.Time) error {
	err := c.Conn.SetWriteDeadline(deadline)
	if err == nil {
		c.mu.Lock()
		c.writeDeadline = deadline
		c.mu.Unlock()
	}
	return err
}

func (c *diagnosticConn) SetDeadline(deadline time.Time) error {
	err := c.Conn.SetDeadline(deadline)
	if err == nil {
		c.mu.Lock()
		c.readDeadline = deadline
		c.writeDeadline = deadline
		c.mu.Unlock()
	}
	return err
}

func (c *diagnosticConn) Close() error {
	c.mu.Lock()
	if c.diagnostics.ClosedAt.IsZero() {
		c.diagnostics.ClosedAt = time.Now()
	}
	c.mu.Unlock()
	return c.Conn.Close()
}
