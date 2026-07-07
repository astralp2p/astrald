package gateway

import (
	"errors"
	"fmt"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/gateway"
)

var _ exonetmod.Parser = &Module{}

// Parse decodes a "gatewayID:targetID" address string into a gateway Endpoint.
// Returns an error if either identity is unresolvable or if gatewayID equals targetID.
func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	if network != NetworkName {
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	var ids = strings.SplitN(address, ":", 2)
	if len(ids) != 2 {
		return nil, fmt.Errorf("invalid endpoint: %s", address)
	}

	var err error
	var endpoint gateway.Endpoint

	endpoint.GatewayID, err = mod.Dir.ResolveIdentity(ids[0])
	if err != nil {
		return nil, err
	}
	endpoint.TargetID, err = mod.Dir.ResolveIdentity(ids[1])
	if err != nil {
		return nil, err
	}

	if endpoint.GatewayID.IsEqual(endpoint.TargetID) {
		return nil, errors.New("invalid endpoint")
	}

	return &endpoint, nil
}
