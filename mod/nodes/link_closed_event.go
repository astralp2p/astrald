package nodes

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type LinkClosedEvent struct {
	RemoteIdentity *astral.Identity // Identity of the other party
	Forced         astral.Bool
	LinkCount      astral.Uint8
}

func (LinkClosedEvent) ObjectType() string {
	return "mod.nodes.link_closed_event"
}

func (e LinkClosedEvent) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *LinkClosedEvent) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (m LinkClosedEvent) MarshalJSON() ([]byte, error) {
	type alias LinkClosedEvent
	return json.Marshal(alias(m))
}

func (m *LinkClosedEvent) UnmarshalJSON(b []byte) error {
	type alias LinkClosedEvent
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = LinkClosedEvent(a)
	return nil
}

func init() {
	_ = astral.Add(&LinkClosedEvent{})
}
