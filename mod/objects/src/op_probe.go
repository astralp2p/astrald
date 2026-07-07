package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opProbeArgs struct {
	ID   *astral.ObjectID `query:"optional"`
	Repo string           `query:"optional"`
	In   string           `query:"optional"`
	Out  string           `query:"optional"`
}

// OpProbe probes a single object when args.ID is set, otherwise streams probes
// for ObjectIDs received over the channel until EOS.
func (mod *Module) OpProbe(ctx *astral.Context, q *routing.IncomingQuery, args opProbeArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	repo := mod.ReadDefault()

	if len(args.Repo) > 0 {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return ch.Send(astral.NewError("repository not found"))
		}
	}

	if args.ID != nil {
		probe, err := mod.Probe(ctx, repo, args.ID)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
		return ch.Send(probe)
	}

	return ch.Handle(ctx, func(object astral.Object) {
		switch msg := object.(type) {
		case *astral.ObjectID:
			probe, err := mod.Probe(ctx, repo, msg)
			if err != nil {
				ch.Send(astral.NewError(err.Error()))
			} else {
				ch.Send(probe)
			}

		case *astral.EOS:
			ch.Close()
		}
	})
}
