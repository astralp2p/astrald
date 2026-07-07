package nodes

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type LinkCreatedEvent struct {
	RemoteIdentity *astral.Identity
	LinkID         astral.Nonce
	LinkCount      astral.Uint32
}

func (e LinkCreatedEvent) ObjectType() string { return "mod.nodes.link_created_event" }

func (e LinkCreatedEvent) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *LinkCreatedEvent) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e LinkCreatedEvent) MarshalJSON() ([]byte, error) {
	type alias LinkCreatedEvent
	return json.Marshal(alias(e))
}

func (e *LinkCreatedEvent) UnmarshalJSON(b []byte) error {
	type alias LinkCreatedEvent
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*e = LinkCreatedEvent(a)
	return nil
}

func init() {
	_ = astral.Add(&LinkCreatedEvent{})
}
