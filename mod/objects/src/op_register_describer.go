package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type opRegisterDescriberArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpRegisterDescriber registers the caller as an external describer.
// Rejects network-origin callers and self-registration by the node.
func (mod *Module) OpRegisterDescriber(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterDescriberArgs) error {
	// Keep this local for now; extract shared external registration validation once the API settles.
	if q.Origin() == astral.OriginNetwork {
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
