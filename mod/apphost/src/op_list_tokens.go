package apphost

import (
	"slices"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opListTokensArgs struct {
	ID  *astral.Identity `query:"optional"`
	Out string           `query:"optional"`
}

// OpListTokens lists all access tokens of an identity
func (mod *Module) OpListTokens(ctx *astral.Context, q *routing.IncomingQuery, args opListTokensArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// get token list
	tokens, err := mod.ListAccessTokens()
	if err != nil {
		ch.Send(astral.NewError("internal error"))
		return err
	}

	// filter tokens by ID
	if !args.ID.IsZero() {
		tokens = slices.DeleteFunc(tokens, func(token *apphost.AccessToken) bool {
			return !token.Identity.IsEqual(args.ID)
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
