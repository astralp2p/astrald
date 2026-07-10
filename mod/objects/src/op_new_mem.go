package objects

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

type opNewMemArgs struct {
	Name string
	Size string `query:"optional"`
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpNewMem(ctx *astral.Context, q *routing.IncomingQuery, args opNewMemArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// parse the size
	size := astral.Size(mem.DefaultSize)
	if len(args.Size) > 0 {
		size, err = astral.ParseSize(args.Size)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	// create the repository
	repo := mem.New("Memory ("+args.Name+")", int64(size))
	err = mod.AddRepository(args.Name, repo)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	mod.AddGroup(objects.RepoMemory, args.Name)

	return ch.Send(&astral.Ack{})
}
