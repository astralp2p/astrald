package apphost

import (
	"slices"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListTokensArgs struct {
	Identity string
	Out      string
}

// OpListTokens lists the access tokens of an identity. An omitted identity lists
// every token.
func (mod *Module) OpListTokens(ctx *astral.Context, q *routing.IncomingQuery, args opListTokensArgs) (err error) {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.authorizeAdminManageApps(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var identity *astral.Identity
	if args.Identity != "" {
		identity, err = mod.Dir.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		if identity.IsZero() {
			return ch.Send(astral.NewError("missing identity"))
		}
	}

	// get token list
	tokens, err := mod.ListAccessTokens()
	if err != nil {
		ch.Send(astral.NewError("internal error"))
		return err
	}

	// filter tokens by identity
	if identity != nil {
		tokens = slices.DeleteFunc(tokens, func(token *apphost.AccessToken) bool {
			return !token.Identity.IsEqual(identity)
		})
	}

	for _, token := range tokens {
		err = ch.Send(token)
		if err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
