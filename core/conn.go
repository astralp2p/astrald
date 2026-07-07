package core

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/astral"
)

type conn struct {
	router *Router
	query  *astral.InFlightQuery
	src    *writer
	dst    *writer
	closed atomic.Bool
	mu     sync.Mutex
}

func newConn(r *Router, q *astral.InFlightQuery) *conn {
	return &conn{
		router: r,
		query:  q,
	}
}

func (c *conn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.src.close()
	c.dst.close()
	c.router.conns.Delete(c.query.Nonce)
	return nil
}

type writer struct {
	conn   *conn
	closed atomic.Bool
	w      io.WriteCloser
	n      int64
}

func newWriter(c *conn, w io.WriteCloser) *writer {
	return &writer{
		conn: c,
		w:    w,
	}
}

func (w *writer) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.n += int64(n)
	return
}

// close closes the underlying writer without triggering conn.Close.
func (w *writer) close() {
	if w.closed.CompareAndSwap(false, true) {
		w.w.Close()
	}
}

// Close closes the writer and the entire connection.
func (w *writer) Close() error {
	w.close()
	w.conn.Close()
	return nil
}
