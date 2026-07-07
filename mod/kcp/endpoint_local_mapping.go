package kcp

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &EndpointLocalMapping{}

type EndpointLocalMapping struct {
	Address astral.String8
	Port    astral.Uint16
}

func (e EndpointLocalMapping) ObjectType() string {
	return "mod.kcp.endpoint_local_mapping"
}

func (e EndpointLocalMapping) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e EndpointLocalMapping) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// MarshalJSON encodes EndpointLocalMapping into JSON.
func (p EndpointLocalMapping) MarshalJSON() ([]byte, error) {
	type alias EndpointLocalMapping
	return json.Marshal(alias(p))
}

// UnmarshalJSON decodes EndpointLocalMapping from JSON.
func (p *EndpointLocalMapping) UnmarshalJSON(bytes []byte) error {
	type alias EndpointLocalMapping
	var a alias
	if err := json.Unmarshal(bytes, &a); err != nil {
		return err
	}
	*p = EndpointLocalMapping(a)
	return nil
}

func init() {
	_ = astral.Add(&EndpointLocalMapping{})
}
