package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func ListTokens(ctx *astral.Context, identity *astral.Identity) ([]*apphost.AccessToken, error) {
	return Default().ListTokens(ctx, identity)
}

// ListTokens returns all access tokens; passing a zero identity omits the identity filter.
func (client *Client) ListTokens(ctx *astral.Context, identity *astral.Identity) (tokens []*apphost.AccessToken, err error) {
	args := query.Args{}
	if !identity.IsZero() {
		args["id"] = identity
	}

	ch, err := client.queryCh(ctx, apphost.MethodListTokens, args)
	if err != nil {
		return
	}
	defer ch.Close()

	err = ch.Switch(
		channel.Collect[*apphost.AccessToken](&tokens),
		channel.PassErrors,
		channel.BreakOnEOS,
	)
	return
}
