package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// QueryRejectedMsg represents a rejection of a query.
type QueryRejectedMsg struct {
	Code astral.Uint8
}

func (QueryRejectedMsg) ObjectType() string {
	return "mod.apphost.query_rejected_msg"
}

func (q QueryRejectedMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&q).WriteTo(w)
}

func (q *QueryRejectedMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(q).ReadFrom(r)
}

func init() {
	_ = astral.Add(&QueryRejectedMsg{})
}
