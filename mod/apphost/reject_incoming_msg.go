package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// RejectIncomingMsg tells the host that the guest will not handle the named inbound
// query. Sent on the registration WS. The caller will see the query rejected with Code.
type RejectIncomingMsg struct {
	QueryID astral.Nonce
	Code    astral.Uint8
}

func (RejectIncomingMsg) ObjectType() string { return "mod.apphost.reject_incoming_msg" }

func (msg RejectIncomingMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *RejectIncomingMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&RejectIncomingMsg{})
}
