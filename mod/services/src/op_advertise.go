package services

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAdvertiseArgs struct {
	Name string

	In  string
	Out string
}

var (
	errAdvertiseNoName   = errors.New("name is required")
	errAdvertiseIdentity = errors.New("invalid caller identity")
	errAdvertiseSelf     = errors.New("the node cannot advertise to itself")
)

// OpAdvertise advertises a named service on behalf of the caller and keeps it
// standing for as long as the channel is open. Closing the channel withdraws
// it, so an app that dies or disconnects takes its advertisement with it and
// leaves no claim for a consumer to trip over.
//
// The provider is the caller, never an argument, so an app can advertise
// nothing but itself.
//
// Objects sent on the channel after the ack replace the advertised info. The
// service stays available across such a change: a follower sees the
// advertisement amended rather than withdrawn and re-raised.
//
// Network-origin queries are rejected: an app extends the node hosting it.
func (mod *Module) OpAdvertise(ctx *astral.Context, q *routing.IncomingQuery, args opAdvertiseArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	id := q.Caller()

	var err error
	switch {
	case len(args.Name) == 0:
		err = errAdvertiseNoName
	case id == nil || id.IsZero():
		err = errAdvertiseIdentity
	case id.IsEqual(mod.node.Identity()):
		err = errAdvertiseSelf
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.external.advertise(id, args.Name, nil)
	defer mod.external.withdraw(id, args.Name)

	if err = ch.Send(&astral.Ack{}); err != nil {
		return err
	}

	err = ch.Switch(
		channel.WithContext(ctx),
		func(info *astral.Bundle) error {
			mod.external.advertise(id, args.Name, info)
			return nil
		},
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}

	return err
}
