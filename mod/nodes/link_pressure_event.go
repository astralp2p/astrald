package nodes

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type LinkPressureEvent struct {
	RemoteIdentity *astral.Identity
	LinkID         astral.Nonce
}

func (LinkPressureEvent) ObjectType() string { return "mod.nodes.link_pressure_event" }

func (e LinkPressureEvent) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *LinkPressureEvent) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e LinkPressureEvent) MarshalJSON() ([]byte, error) {
	type alias LinkPressureEvent
	return json.Marshal(alias(e))
}

func (e *LinkPressureEvent) UnmarshalJSON(b []byte) error {
	type alias LinkPressureEvent
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*e = LinkPressureEvent(a)
	return nil
}

func init() {
	_ = astral.Add(&LinkPressureEvent{})
}
