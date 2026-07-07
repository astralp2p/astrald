package nodes

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type NewObservedEndpointEvent struct{}

func (m NewObservedEndpointEvent) ObjectType() string {
	return "mod.nodes.new_observed_endpoint_event"
}

func (m NewObservedEndpointEvent) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&m).WriteTo(w)
}

func (m *NewObservedEndpointEvent) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(m).ReadFrom(r)
}

func (m NewObservedEndpointEvent) MarshalJSON() ([]byte, error) {
	type alias NewObservedEndpointEvent
	return json.Marshal(alias(m))
}

func (m *NewObservedEndpointEvent) UnmarshalJSON(b []byte) error {
	type alias NewObservedEndpointEvent
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = NewObservedEndpointEvent(a)
	return nil
}

func init() {
	_ = astral.Add(&NewObservedEndpointEvent{})
}
