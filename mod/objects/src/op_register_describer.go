package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/objects"
)

type opRegisterDescriberArgs struct {
	In  string
	Out string
}

// OpRegisterDescriber authorizes the caller under ServeObjects for the describer role, then
// registers it as an external describer.
// Rejects network-origin callers and self-registration by the node.
func (mod *Module) OpRegisterDescriber(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterDescriberArgs) error {
	// why: cheapest refusal first. A network caller is refused whatever it holds,
	// and this costs no database work — Authorize below queries the grants and may
	// then walk the contract chain, and it logs an allow before this would have
	// rejected. It is not the authorization: a query routed locally by a tool
	// carries no network origin whatever its caller.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Role:   auth.RoleDescriber,
	}) {
		return q.Reject()
	}

	id := q.Caller()
	var err error
	switch {
	case id == nil || id.IsZero():
		err = objects.ErrInvalidSourceIdentity
	case id.IsEqual(mod.node.Identity()):
		err = objects.ErrExternalRegistrationSelf
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.AddDescriber(NewExternalDescriber(mod, id))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
