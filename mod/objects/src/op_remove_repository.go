package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveRepositoryArgs struct {
	Name string `query:"required"`
	In   string
	Out  string
}

func (mod *Module) OpRemoveRepository(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveRepositoryArgs) (err error) {
	// why AdminObjects and not StoreObjects: a grant to add objects does not grant
	// destroying them.
	if !mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(args.Name),
	}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.RemoveRepository(args.Name)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
