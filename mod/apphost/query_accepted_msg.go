package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// QueryAcceptedMsg represents a success response to a query
type QueryAcceptedMsg struct {
}

func (QueryAcceptedMsg) ObjectType() string { return "mod.apphost.query_accepted_msg" }

func (msg QueryAcceptedMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *QueryAcceptedMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&QueryAcceptedMsg{})
}
