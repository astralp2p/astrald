package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Endpoint identifies a routable destination through a specific gateway node:
// traffic to TargetID is forwarded via GatewayID over the "gw" exonet network.
type Endpoint struct {
	GatewayID *astral.Identity
	TargetID  *astral.Identity
}

var _ exonet.Endpoint = &Endpoint{}

func (Endpoint) ObjectType() string { return "mod.gateway.endpoint" }

func (e Endpoint) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *Endpoint) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// NewEndpoint makes a new Endpoint
func NewEndpoint(gateway *astral.Identity, target *astral.Identity) *Endpoint {
	return &Endpoint{GatewayID: gateway, TargetID: target}
}

// exonet.Endpoint

func (e Endpoint) Network() string {
	return "gw"
}

// Address returns a text representation of the address
func (e Endpoint) Address() string {
	return e.GatewayID.String() + ":" + e.TargetID.String()
}

// Pack returns a binary representation of the address
func (e Endpoint) Pack() []byte {
	buf := &bytes.Buffer{}

	_, err := e.WriteTo(buf)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

// text support

func (e *Endpoint) UnmarshalText(text []byte) (err error) {
	ids := strings.SplitN(string(text), ":", 2)
	if len(ids) != 2 {
		return errors.New("malformed endpoint")
	}

	e.GatewayID, err = astral.ParseIdentity(ids[0])
	if err != nil {
		return
	}

	e.TargetID, err = astral.ParseIdentity(ids[1])

	return
}

func (e Endpoint) MarshalText() (text []byte, err error) {
	return []byte(e.Address()), nil
}

// json support

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Address())
}

func (e *Endpoint) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	ep, err := ParseEndpoint(s)
	if err != nil {
		return err
	}
	*e = *ep
	return nil
}

// ParseEndpoint parses a gateway endpoint from the canonical "<gatewayID>:<targetID>" text form.
func ParseEndpoint(s string) (*Endpoint, error) {
	// expected: <gateway_id>:<target_id>
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed endpoint")
	}

	gw, err := astral.ParseIdentity(parts[0])
	if err != nil {
		return nil, err
	}

	target, err := astral.ParseIdentity(parts[1])
	if err != nil {
		return nil, err
	}

	return &Endpoint{
		GatewayID: gw,
		TargetID:  target,
	}, nil
}

// other

func (e Endpoint) IsZero() bool {
	return e.GatewayID.IsZero() && e.TargetID.IsZero()
}

func (e Endpoint) String() string {
	return e.Network() + ":" + e.Address()
}

func init() {
	astral.Add(&Endpoint{})
}
