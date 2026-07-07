package astrald

import (
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/astral"
)

// ConnMonitor wraps an astral.Conn to track transferred bytes and fire optional callbacks on close or I/O errors.
// Callbacks are invoked synchronously at the point of the error or close call.
type ConnMonitor struct {
	OnClose      func()
	OnReadError  func(error)
	OnWriteError func(error)
	conn         astral.Conn
	query        *astral.InFlightQuery
	bytesRead    atomic.Uint64
	bytesWritten atomic.Uint64
}

var _ astral.Conn = &ConnMonitor{}

func NewConnMonitor(conn astral.Conn, query *astral.InFlightQuery) *ConnMonitor {
	return &ConnMonitor{conn: conn, query: query}
}

func (monitor *ConnMonitor) Read(b []byte) (n int, err error) {
	n, err = monitor.conn.Read(b)
	monitor.bytesRead.Add(uint64(n))
	if err != nil && monitor.OnReadError != nil {
		monitor.OnReadError(err)
	}
	return
}

func (monitor *ConnMonitor) Write(b []byte) (n int, err error) {
	n, err = monitor.conn.Write(b)
	monitor.bytesWritten.Add(uint64(n))
	if err != nil && monitor.OnWriteError != nil {
		monitor.OnWriteError(err)
	}
	return
}

func (monitor *ConnMonitor) Close() error {
	if monitor.OnClose != nil {
		defer monitor.OnClose()
	}

	return monitor.conn.Close()
}

func (monitor *ConnMonitor) LocalIdentity() *astral.Identity {
	return monitor.conn.LocalIdentity()
}

func (monitor *ConnMonitor) RemoteIdentity() *astral.Identity {
	return monitor.conn.RemoteIdentity()
}

func (monitor *ConnMonitor) BytesRead() astral.Size {
	return astral.Size(monitor.bytesRead.Load())
}

func (monitor *ConnMonitor) BytesWritten() astral.Size {
	return astral.Size(monitor.bytesWritten.Load())
}

func (monitor *ConnMonitor) Query() *astral.InFlightQuery {
	return monitor.query
}
