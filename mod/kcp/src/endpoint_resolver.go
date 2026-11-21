package kcp

import (
	ipmod "github.com/astralp2p/astrald/mod/ip"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns KCP endpoints only for the local node identity while
// kcp listens; any other identity, or a listen of false, yields an empty channel.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	if !nodeID.IsEqual(mod.node.Identity()) {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	listen := mod.settings.Listen.Get()
	if listen != nil && !*listen {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	return sig.ArrayToChan(mod.endpoints()), nil
}

func (mod *Module) endpoints() (list []*nodes.EndpointWithTTL) {
	ips, _ := mod.IP.LocalIPs()
	for _, tip := range ips {
		e := &kcp.Endpoint{
			IP:   tip,
			Port: astral.Uint16(mod.config.ListenPort),
		}

		list = append(list, nodes.NewEndpointWithTTL(e, ipmod.EndpointTTL(tip)))
	}

	return list
}
