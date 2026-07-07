package nodes

import (
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
)

var _ exonet.Endpoint = &EndpointWithTTL{}
var _ astral.Object = &EndpointWithTTL{}

// EndpointWithTTL pairs an exonet.Endpoint with an optional TTL in seconds.
// A nil TTL means the endpoint does not expire.
type EndpointWithTTL struct {
	Endpoint exonet.Endpoint
	TTL      *astral.Uint32 // seconds, nil = no expiry
}

func NewEndpointWithTTL(endpoint exonet.Endpoint, ttl ...time.Duration) *EndpointWithTTL {
	re := &EndpointWithTTL{Endpoint: endpoint}
	if len(ttl) > 0 {
		var secs astral.Uint32 = 0
		for _, d := range ttl {
			secs += astral.Uint32(d.Seconds())
		}

		re.TTL = &secs
	}

	return re
}

func (EndpointWithTTL) ObjectType() string {
	return "mod.nodes.endpoint_with_ttl"
}

func (e EndpointWithTTL) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *EndpointWithTTL) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// exonet.Endpoint

func (e EndpointWithTTL) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&e).MarshalJSON()
}

func (e *EndpointWithTTL) UnmarshalJSON(b []byte) error {
	return astral.Objectify(e).UnmarshalJSON(b)
}

func (e *EndpointWithTTL) Network() string { return e.Endpoint.Network() }
func (e *EndpointWithTTL) Address() string { return e.Endpoint.Address() }
func (e *EndpointWithTTL) Pack() []byte    { return e.Endpoint.Pack() }

func init() {
	_ = astral.Add(&EndpointWithTTL{})
}
