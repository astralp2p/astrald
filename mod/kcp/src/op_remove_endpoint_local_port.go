package kcp

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveEndpointLocalPort struct {
	Endpoint astral.String8
	In       string
	Out      string
}

func (mod *Module) OpRemoveEndpointLocalPort(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveEndpointLocalPort) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	endpoint, err := kcp.ParseEndpoint(string(args.Endpoint))
	if err != nil {
		ch := q.Accept(channel.WithFormats(args.In, args.Out))
		defer ch.Close()
		return ch.Send(kcp.ErrInvalidEndpoint)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.RemoveEndpointLocalSocket(*endpoint)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
