package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// IncomingQueryMsg notifies a registered service WS of an inbound query awaiting handling.
// The guest must either attach (open a per-query connection sending AttachQueryMsg with
// the same QueryID) or send RejectIncomingMsg on the registration WS within the attach
// timeout. Ignoring it results in route-not-found for the caller.
type IncomingQueryMsg struct {
	QueryID astral.Nonce
	Caller  *astral.Identity
	Target  *astral.Identity
	Query   astral.String16
}

func (IncomingQueryMsg) ObjectType() string { return "mod.apphost.incoming_query_msg" }

func (msg IncomingQueryMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *IncomingQueryMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&IncomingQueryMsg{})
}
