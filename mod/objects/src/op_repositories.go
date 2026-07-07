package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opRepositoriesArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpRepositories(ctx *astral.Context, q *routing.IncomingQuery, args opRepositoriesArgs) (err error) {
	ctx = ctx.ExcludeZone(astral.ZoneNetwork)

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for name, repo := range mod.repos.Clone() {
		free, _ := repo.Free(ctx)

		err = ch.Send(&objects.RepositoryInfo{
			Name:  astral.String8(name),
			Label: astral.String8(repo.Label()),
			Free:  astral.Uint64(free),
		})
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
