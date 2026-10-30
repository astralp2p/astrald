package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opListHeldObjectsArgs struct {
	Out string
}

// why: no action: the op lists only the caller's own hold rows.
func (mod *Module) OpListHeldObjects(ctx *astral.Context, q *routing.IncomingQuery, args opListHeldObjectsArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	rows, err := mod.db.ListHeldObjects(q.Caller())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, row := range rows {
		if err := ch.Send(row.ObjectID); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
