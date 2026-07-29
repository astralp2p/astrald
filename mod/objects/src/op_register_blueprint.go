package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterBlueprintArgs struct {
	In  string
	Out string
}

// OpRegisterBlueprint runs in batch mode: reads runtime *astral.Blueprint descriptors
// (struct kind or alias kind) from the channel until EOS or EOF, performs registration for
// each, sends the resulting ObjectID or an error per input. An explicit EOS
// input is answered with a final EOS; a stream ended by EOF is not.
//
// todo(security): gate by caller identity before mutating DefaultBlueprints. Any peer can
// currently (a) squat a victim type name to permanently block legitimate registration,
// and (b) define the wire schema for that name. Needs an authorization hook (allowlist of
// identities, per-identity quotas, or a capability check) before reaching mod.Register.
func (mod *Module) OpRegisterBlueprint(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterBlueprintArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(bp *astral.Blueprint) astral.Object {
		id, regErr := mod.Register(bp)
		if regErr != nil {
			return astral.NewError(regErr.Error())
		}
		return id
	})
}
