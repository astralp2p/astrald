package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opRepositoriesArgs struct {
	Out string
}

func (mod *Module) OpRepositories(ctx *astral.Context, q *routing.IncomingQuery, args opRepositoriesArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ctx = ctx.ExcludeZone(astral.ZoneNetwork)

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for name, repo := range mod.repos.Clone() {
		err = ch.Send(newRepositoryInfo(ctx, name, repo))
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}

// newRepositoryInfo describes one registered repository; a group lists its direct members by name.
func newRepositoryInfo(ctx *astral.Context, name string, repo objectsmod.Repository) *objects.RepositoryInfo {
	free, _ := repo.Free(ctx)

	info := &objects.RepositoryInfo{
		Name:     astral.String8(name),
		Label:    astral.String8(repo.Label()),
		Free:     astral.Uint64(free),
		Kind:     objects.RepositoryKindRepository,
		Children: []astral.String8{},
	}

	group, ok := repo.(*RepoGroup)
	if !ok {
		return info
	}

	info.Kind = objects.RepositoryKindGroup
	info.Concurrent = astral.Bool(group.Concurrent)
	for _, child := range group.List() {
		info.Children = append(info.Children, astral.String8(child))
	}

	return info
}
