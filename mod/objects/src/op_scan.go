package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opScanArgs struct {
	Repo   string
	Follow bool         `query:"optional"`
	Zone   *astral.Zone `query:"optional"`
	Out    string       `query:"optional"`
}

// OpScan sends a list of object ids in a repository
func (mod *Module) OpScan(ctx *astral.Context, q *routing.IncomingQuery, args opScanArgs) (err error) {
	// prepare the context
	ctx = ctx.WithIdentity(q.Caller())
	if args.Zone == nil {
		ctx = ctx.WithZone(astral.ZoneAll)
	} else {
		ctx = ctx.WithZone(*args.Zone)
	}

	ctx, cancel := ctx.WithCancel()
	defer cancel()

	// accept
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// look up the repository
	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.NewError("repository not found"))
	}

	// start the scan
	scan, err := repo.Scan(ctx, args.Follow)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	// if the channel closes, cancel our scan context
	go func() {
		for {
			_, err := ch.Receive()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// forward scan results
	for id := range scan {
		if id == nil {
			err = ch.Send(&astral.EOS{})
		} else {
			err = ch.Send(id)
		}
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
