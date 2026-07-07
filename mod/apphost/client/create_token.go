package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func CreateToken(ctx *astral.Context, identity *astral.Identity) (*apphost.AccessToken, error) {
	return Default().CreateToken(ctx, identity)
}

// CreateToken requests a new access token scoped to the given identity.
func (client *Client) CreateToken(ctx *astral.Context, identity *astral.Identity) (token *apphost.AccessToken, err error) {
	ch, err := client.queryCh(ctx, apphost.MethodCreateToken, query.Args{"id": identity.String()})
	if err != nil {
		return
	}
	defer ch.Close()
	err = ch.Switch(channel.Expect(&token), channel.PassErrors)
	return
}
