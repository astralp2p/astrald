package kcp

import (
	"fmt"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"net"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/kcp"
	"github.com/pkg/errors"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

var _ exonetmod.Dialer = &Module{}

// Dial establishes a KCP session and wraps it as an exonet.Conn.
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (
	c exonetmod.Conn, err error) {
	if endpoint.Network() != "kcp" {
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	if dial := mod.settings.Dial.Get(); dial != nil && !*dial {
		return nil, exonetmod.ErrDisabledNetwork
	}

	remoteEndpoint, ok := endpoint.(*kcp.Endpoint)
	if !ok {
		return nil, fmt.Errorf("kcp/dial: endpoint is not a kcp endpoint")
	}

	udpConn, err := mod.prepareUDPConn(remoteEndpoint)
	if err != nil {
		return nil, err
	}

	kcpConn, err := kcpgo.NewConn(endpoint.Address(), nil, 0, 0, udpConn)
	if err != nil {
		_ = udpConn.Close()
		return nil, fmt.Errorf("kcp/dial: creating KCP conn failed: %w", err)
	}

	defer func() {
		if err != nil {
			_ = kcpConn.Close()
		}
	}()

	localEndpoint, err := kcp.ParseEndpoint(kcpConn.LocalAddr().String())
	if err != nil {
		return nil, fmt.Errorf("kcp/dial: parsing local endpoint failed: %w", err)
	}

	return WrapKCPConn(kcpConn, remoteEndpoint, localEndpoint, true, mod.config.DialTimeout), nil
}

// SetEndpointLocalSocket pins a local UDP port to a remote KCP endpoint address.
// When replace is false, it fails if a mapping already exists; when true, it overwrites.
func (mod *Module) SetEndpointLocalSocket(endpoint kcp.Endpoint, localSocket astral.Uint16, replace astral.Bool) error {
	address := astral.String8(endpoint.Address())

	if replace {
		mod.ephemeralPortMappings.Replace(address, localSocket)
		return nil
	}

	_, ok := mod.ephemeralPortMappings.Set(address, localSocket)
	if !ok {
		return fmt.Errorf("%w: address %s", kcp.ErrEndpointLocalSocketExists, address)
	}

	return nil
}

func (mod *Module) RemoveEndpointLocalSocket(endpoint kcp.Endpoint) error {
	address := astral.String8(endpoint.Address())

	mod.ephemeralPortMappings.Delete(address)
	return nil
}

func (mod *Module) GetEndpointsMappings() map[astral.String8]astral.Uint16 {
	return mod.ephemeralPortMappings.Clone()
}

// prepareUDPConn creates a UDP connection, binding to an ephemeral local port if mapped.
func (mod *Module) prepareUDPConn(endpoint *kcp.Endpoint) (*net.UDPConn, error) {
	laddr := &net.UDPAddr{Port: 0}
	address := astral.String8(endpoint.Address())

	// Use mapped local port if available
	if port, ok := mod.ephemeralPortMappings.Get(address); ok {
		laddr.Port = int(port)
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, errors.Wrap(err, "kcp/dial: binding to local UDP address")
	}

	return conn, nil
}
