package dir

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAliasArgs struct {
	Identity string  `query:"required"`
	Alias    *string `query:"required"` // required but can be empty
	Out      string
}

func (mod *Module) OpSetAlias(ctx *astral.Context, q *routing.IncomingQuery, args opSetAliasArgs) (err error) {
	// why: the check sits at the op and not in Module.SetAlias, because SetAlias
	// also serves in-process callers, which carry no query caller to authorize.
	// note: clearing an alias is a change and passes the same check as setting one.
	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	identity, err := mod.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if identity.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	err = mod.SetAlias(identity, *args.Alias)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
