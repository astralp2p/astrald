package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"gorm.io/gorm"
)

type opDeleteTokenArgs struct {
	Token astral.String8
	Out   string `query:"optional"`
}

// OpDeleteToken deletes an access token so it no longer authenticates
func (mod *Module) OpDeleteToken(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteTokenArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.Token == "" {
		return ch.Send(astral.NewError("missing token"))
	}

	if err := mod.DeleteAccessToken(string(args.Token)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ch.Send(astral.NewError("token not found"))
		}
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
