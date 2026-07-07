package query

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Conn is a basic implementation of an astral.Conn over a Reader and a WriteCloser.
type Conn struct {
	localID  *astral.Identity
	remoteID *astral.Identity
	io.WriteCloser
	io.Reader
	outbound bool
}

var _ astral.Conn = &Conn{}

func NewConn(localID *astral.Identity, remoteID *astral.Identity, w io.WriteCloser, r io.Reader, outbound bool) astral.Conn {
	return &Conn{
		localID:     localID,
		remoteID:    remoteID,
		WriteCloser: w,
		Reader:      r,
		outbound:    outbound,
	}
}

// Read closes the connection on any read error, ensuring the write side is also torn down.
func (s *Conn) Read(p []byte) (n int, err error) {
	n, err = s.Reader.Read(p)
	if err != nil {
		s.Close()
	}
	return n, err
}

func (s *Conn) Outbound() bool {
	return s.outbound
}

func (s *Conn) RemoteIdentity() *astral.Identity {
	return s.remoteID
}

func (s *Conn) LocalIdentity() *astral.Identity {
	return s.localID
}
