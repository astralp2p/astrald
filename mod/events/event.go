package events

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type Event struct {
	ID        astral.Nonce
	SourceID  *astral.Identity
	Timestamp astral.Time
	Data      astral.Object
}

func (Event) ObjectType() string {
	return "mod.events.event"
}

func (e Event) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *Event) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func init() {
	_ = astral.Add(&Event{})
}
