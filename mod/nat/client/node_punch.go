package nat

import (
	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/api/nat"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// NodePunch coordinates the offer/answer/ready/go signalling exchange with the NAT module, then delegates
// the actual UDP hole-punch to puncher; returns the resulting Hole on success.
func (t *Client) NodePunch(ctx *astral.Context, target *astral.Identity, localIP ip.IP, puncher nat.Puncher) (*nat.Hole, error) {
	ch, err := t.queryCh(ctx, nat.MethodNodePunch, query.Args{})
	if err != nil {
		return nil, err
	}

	defer ch.Close()

	proto := nat.NewPunchProtocol(t.astral.GuestID(), target, localIP)
	localPort, err := puncher.Open()
	if err != nil {
		return nil, err
	}

	proto.LocalPort = astral.Uint16(localPort)
	proto.Session = puncher.Session()

	if err := ch.Send(proto.OfferSignal()); err != nil {
		return nil, err
	}

	err = ch.Switch(
		proto.ExpectSignal(nat.PunchSignalTypeAnswer, proto.OnAnswer),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	if err := ch.Send(proto.ReadySignal()); err != nil {
		return nil, err
	}

	err = ch.Switch(
		proto.ExpectSignal(nat.PunchSignalTypeGo, nil),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	result, err := puncher.HolePunch(ctx, proto.PeerIP, int(proto.PeerPort))
	if err != nil {
		return nil, err
	}

	proto.SetPunchResult(result)
	proto.Hole.Nonce = astral.NewNonce()

	if err := ch.Send(proto.ResultSignal()); err != nil {
		return nil, err
	}

	err = ch.Switch(
		proto.ExpectSignal(nat.PunchSignalTypeResult, proto.OnResult),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	return &proto.Hole, nil
}
