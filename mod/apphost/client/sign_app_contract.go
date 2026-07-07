package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

func SignAppContract(ctx *astral.Context, contract *auth.Contract) (*auth.SignedContract, error) {
	return Default().SignAppContract(ctx, contract)
}

// SignAppContract sends the contract over the channel body (not query args) and returns the signed result.
func (client *Client) SignAppContract(ctx *astral.Context, contract *auth.Contract) (signed *auth.SignedContract, err error) {
	ch, err := client.queryCh(ctx, apphost.MethodSignAppContract, nil)
	if err != nil {
		return
	}
	defer ch.Close()

	err = ch.Send(contract)
	if err != nil {
		return
	}

	err = ch.Switch(channel.Expect(&signed), channel.PassErrors)
	return
}
