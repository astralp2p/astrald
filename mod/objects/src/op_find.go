package objects

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

type opFindArgs struct {
	ID   *astral.ObjectID `query:"required"`
	Zone *astral.Zone
	Out  string
}

func (mod *Module) OpFind(ctx *astral.Context, q *routing.IncomingQuery, args opFindArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).WithTimeout(time.Minute)
	defer cancel()

	if args.Zone != nil {
		ctx = ctx.LimitZone(*args.Zone)
	}

	// why: a query from the network answers from this node alone. Its sender
	// already asks every swarm member, and a second hop sends it back (objects.find spec).
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	if args.ID == nil || args.ID.IsZero() {
		return ch.Send(astral.NewError("id is required"))
	}

	providers, err := mod.Find(ctx, args.ID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	var dup = make(map[string]struct{})

	for {
		provider, ok, err := sig.RecvOk(ctx, providers)
		if err != nil || !ok {
			break
		}

		// deduplicate providers
		key := provider.String()
		if _, found := dup[key]; found {
			continue
		}

		dup[key] = struct{}{}

		if err := ch.Send(provider); err != nil {
			return fmt.Errorf("error writing provider: %w", err)
		}
	}

	return ch.Send(&astral.EOS{})
}
