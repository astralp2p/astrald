package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListAuthorizersArgs struct {
	Out string
}

// OpListAuthorizers sends every persisted external authorizer assignment, then
// EOS. A token value is never sent; a token file or variable is sent by name.
func (mod *Module) OpListAuthorizers(ctx *astral.Context, q *routing.IncomingQuery, args opListAuthorizersArgs) error {
	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, a := range mod.listedAssignments() {
		if err := ch.Send(a); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

// listedAssignments returns copies of the persisted assignments without their
// token values.
func (mod *Module) listedAssignments() []*auth.AuthorizerAssignment {
	var list []*auth.AuthorizerAssignment
	for _, a := range mod.assignments() {
		listed := *a
		listed.TokenValue = ""
		list = append(list, &listed)
	}
	return list
}
