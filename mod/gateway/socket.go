package gateway

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Socket describes a raw connection point at the gateway. The recipient opens
// a raw exonet connection to the Endpoint and sends Nonce as the first bytes
// to identify itself to the gateway.
type Socket struct {
	Endpoint exonet.Endpoint
	Nonce    astral.Nonce
}

func (Socket) ObjectType() string { return "mod.gateway.socket" }

func (s Socket) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&s).WriteTo(w) }
func (s *Socket) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(s).ReadFrom(r) }

func init() {
	astral.Add(&Socket{})
}
