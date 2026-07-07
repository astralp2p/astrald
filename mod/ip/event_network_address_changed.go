package ip

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type EventNetworkAddressChanged struct {
	Removed []IP
	Added   []IP
	All     []IP
}

// astral

func (EventNetworkAddressChanged) ObjectType() string {
	return "mod.ip.events.network_address_changed"
}

func (e EventNetworkAddressChanged) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *EventNetworkAddressChanged) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// ...

func init() {
	_ = astral.Add(&EventNetworkAddressChanged{})
}
