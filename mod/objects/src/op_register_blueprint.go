package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opRegisterBlueprintArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpRegisterBlueprint runs in batch mode: reads runtime *astral.Blueprint descriptors
// (struct kind or alias kind) from the channel until EOS or EOF, performs registration for
// each, sends the resulting ObjectID or an error per input, then emits a final EOS marker
// before closing.
//
// todo(security): gate by caller identity before mutating DefaultBlueprints. Any peer can
// currently (a) squat a victim type name to permanently block legitimate registration,
// and (b) define the wire schema for that name. Needs an authorization hook (allowlist of
// identities, per-identity quotas, or a capability check) before reaching mod.Register.
func (mod *Module) OpRegisterBlueprint(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterBlueprintArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	register := func(o astral.Object) error {
		id, regErr := mod.Register(o)
		if regErr != nil {
			return ch.Send(astral.NewError(regErr.Error()))
		}
		return ch.Send(id)
	}

	err := ch.Switch(
		func(bp *astral.Blueprint) error { return register(bp) },
		channel.BreakOnEOS,
		func(other astral.Object) error {
			return ch.Send(astral.NewError("expected astral.Blueprint"))
		},
	)
	if err != nil {
		return err
	}
	_ = ch.Send(&astral.EOS{})
	return nil
}
