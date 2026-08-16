package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opProbeArgs struct {
	ID   *astral.ObjectID
	Repo string
	In   string
	Out  string
}

// OpProbe probes a single object when args.ID is set, otherwise streams probes
// for ObjectIDs received over the channel until EOS.
func (mod *Module) OpProbe(ctx *astral.Context, q *routing.IncomingQuery, args opProbeArgs) (err error) {
	if !mod.authorizeSeeObjects(ctx, q, args.ID, args.Repo) {
		return q.Reject()
	}

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

	return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
		probe, err := mod.Probe(ctx, repo, id)
		if err != nil {
			return astral.NewError(err.Error())
		}
		return probe
	}, channel.WithContext(ctx))
}
