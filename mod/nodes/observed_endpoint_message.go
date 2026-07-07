package nodes

import (
	"errors"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
)

type ObservedEndpointMessage struct {
	Endpoint exonet.Endpoint
}

func (ObservedEndpointMessage) ObjectType() string {
	return "mod.nodes.observed_endpoint_message"
}

func (e ObservedEndpointMessage) WriteTo(w io.Writer) (n int64, err error) {
	if e.Endpoint == nil {
		return 0, errors.New("nil endpoint")
	}
	return astral.Encode(w, e.Endpoint)
}

func (e *ObservedEndpointMessage) ReadFrom(r io.Reader) (n int64, err error) {
	obj, m, err := astral.Decode(r)
	n += m
	if err != nil {
		return
	}

	if ep, ok := obj.(exonet.Endpoint); ok {
		e.Endpoint = ep
		return
	}

	return n, fmt.Errorf("object is not an exonet.Endpoint")
}

func init() {
	_ = astral.Add(&ObservedEndpointMessage{})
}
