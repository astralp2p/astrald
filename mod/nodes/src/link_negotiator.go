package nodes

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/nodes/src/noise"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	ErrNoSupportedFeature         = errors.New("no supported link feature")
	ErrUnsupportedSelectedFeature = errors.New("unsupported selected link feature")
	ErrNegotiationRejected        = errors.New("link negotiation rejected")
)

type muxLinkNegotiator struct {
	mod *Module
	ch  *channel.Channel
}

// NegotiateOutbound reads the peer's feature list, selects mux2, and waits for the link nonce; fails if mux2 is absent or rejected.
func (n *muxLinkNegotiator) NegotiateOutbound() (*Link, error) {
	var count *astral.Uint16
	if err := n.ch.Switch(channel.Expect(&count), channel.PassErrors); err != nil {
		return nil, fmt.Errorf("read feature count: %w", err)
	}

	var supported bool
	for i := 0; i < int(*count); i++ {
		var feature *astral.String8
		if err := n.ch.Switch(channel.Expect(&feature), channel.PassErrors); err != nil {
			return nil, fmt.Errorf("read feature: %w", err)
		}
		if feature.String() == featureMux2 {
			supported = true
		}
	}

	if !supported {
		return nil, ErrNoSupportedFeature
	}

	if err := n.ch.Send(astral.NewString8(featureMux2)); err != nil {
		return nil, fmt.Errorf("send selected feature: %w", err)
	}

	var status *astral.Uint8
	if err := n.ch.Switch(channel.Expect(&status), channel.PassErrors); err != nil {
		return nil, fmt.Errorf("read negotiation status: %w", err)
	}
	if *status != 0 {
		return nil, ErrNegotiationRejected
	}

	var nonce *astral.Nonce
	if err := n.ch.Switch(channel.Expect(&nonce), channel.PassErrors); err != nil {
		return nil, fmt.Errorf("read link nonce: %w", err)
	}

	conn, ok := n.ch.Transport().(astral.Conn)
	if !ok {
		return nil, errors.New("negotiation channel transport is not an astral conn")
	}

	link := newLink(n.mod, conn, *nonce, true)
	return link, nil
}

// NegotiateInbound offers mux2, confirms the peer selected it, and assigns the link nonce.
func (n *muxLinkNegotiator) NegotiateInbound() (*Link, error) {
	if err := n.ch.Send(astral.NewUint16(1)); err != nil {
		return nil, fmt.Errorf("send feature count: %w", err)
	}
	if err := n.ch.Send(astral.NewString8(featureMux2)); err != nil {
		return nil, fmt.Errorf("send feature: %w", err)
	}

	var feature *astral.String8
	if err := n.ch.Switch(channel.Expect(&feature), channel.PassErrors); err != nil {
		return nil, fmt.Errorf("read selected feature: %w", err)
	}
	if feature.String() != featureMux2 {
		if err := n.ch.Send(astral.NewUint8(1)); err != nil {
			return nil, fmt.Errorf("send negotiation rejection: %w", err)
		}
		return nil, ErrUnsupportedSelectedFeature
	}

	if err := n.ch.Send(astral.NewUint8(0)); err != nil {
		return nil, fmt.Errorf("send negotiation status: %w", err)
	}

	nonce := astral.NewNonce()
	if err := n.ch.Send(&nonce); err != nil {
		return nil, fmt.Errorf("send link nonce: %w", err)
	}

	conn, ok := n.ch.Transport().(astral.Conn)
	if !ok {
		return nil, errors.New("negotiation channel transport is not an astral conn")
	}

	link := newLink(n.mod, conn, nonce, false)
	return link, nil
}

// EstablishOutboundLink runs the noise handshake and mux negotiation over conn, then registers the link; closes conn on any error.
func (mod *Module) EstablishOutboundLink(ctx context.Context, remoteID *astral.Identity, conn exonet.Conn) (_ nodes.Link, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	privKey, err := mod.getPrivateKey()
	if err != nil {
		return nil, err
	}

	aconn, err := noise.HandshakeOutbound(
		ctx,
		conn,
		remoteID.PublicKey(),
		secp256k1.PrivKeyFromBytes(privKey.Key),
	)
	if err != nil {
		return nil, fmt.Errorf("outbound handshake: %w", err)
	}

	ch := channel.New(aconn, channel.WithLockedWrites())
	negotiator := mod.GetLinkNegotiator(ch)
	link, err := negotiator.NegotiateOutbound()
	if err != nil {
		return nil, err
	}
	if err := mod.linkPool.AddLink(link); err != nil {
		return nil, err
	}
	return link, nil
}

// EstablishInboundLink runs the inbound noise handshake and mux negotiation over conn, then registers the link; closes conn on any error.
func (mod *Module) EstablishInboundLink(ctx context.Context, conn exonet.Conn) (err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	privKey, err := mod.getPrivateKey()
	if err != nil {
		return err
	}

	aconn, err := noise.HandshakeInbound(ctx, conn, secp256k1.PrivKeyFromBytes(privKey.Key))
	if err != nil {
		return err
	}

	ch := channel.New(aconn, channel.WithLockedWrites())
	negotiator := mod.GetLinkNegotiator(ch)
	link, err := negotiator.NegotiateInbound()
	if err != nil {
		return err
	}
	err = mod.linkPool.AddLink(link)
	return err
}
