package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// BindMsg associates a set of handler registrations with a nonce token so the
// host can remove them all when the bind connection closes.
type BindMsg struct {
	Token astral.Nonce
}

func (BindMsg) ObjectType() string { return "mod.apphost.bind_msg" }

func (msg BindMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *BindMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&BindMsg{})
}
