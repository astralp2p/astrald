package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// RegisterServiceMsg registers the guest's connection as the handler for inbound queries
// targeting the given identity. The host pushes IncomingQueryMsg for each inbound query;
// the guest opens a per-query connection (sending AttachQueryMsg) to respond, or sends
// RejectIncomingMsg on this connection to refuse.
type RegisterServiceMsg struct {
	Identity *astral.Identity
}

func (RegisterServiceMsg) ObjectType() string { return "mod.apphost.register_service_msg" }

func (msg RegisterServiceMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *RegisterServiceMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&RegisterServiceMsg{})
}
