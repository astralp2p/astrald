package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// RegisterHandlerMsg represents a request to register a query handler for an identity
type RegisterHandlerMsg struct {
	Identity  *astral.Identity
	Endpoint  astral.String8 // IPC endpoint
	AuthToken astral.Nonce
}

func (RegisterHandlerMsg) ObjectType() string {
	return "mod.apphost.register_handler_msg"
}

func (msg RegisterHandlerMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *RegisterHandlerMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&RegisterHandlerMsg{})
}
