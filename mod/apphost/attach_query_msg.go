package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// AttachQueryMsg attaches a freshly-opened per-query WS to a pending inbound query
// announced earlier via IncomingQueryMsg. On success the host replies with Ack and the
// connection becomes the responder bytestream for that query.
type AttachQueryMsg struct {
	QueryID astral.Nonce
}

func (AttachQueryMsg) ObjectType() string { return "mod.apphost.attach_query_msg" }

func (msg AttachQueryMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *AttachQueryMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&AttachQueryMsg{})
}
