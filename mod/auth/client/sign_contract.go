package auth

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// SignContract submits contract for remote signing; the contract is sent after the channel is
// established, not as query args.
func (c *Client) SignContract(ctx *astral.Context, contract *auth.Contract) (*auth.SignedContract, error) {
	ch, err := c.queryCh(ctx, auth.MethodSignContract, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	if err = ch.Send(contract); err != nil {
		return nil, err
	}

	var signed *auth.SignedContract
	err = ch.Switch(channel.Expect(&signed), channel.PassErrors)
	return signed, err
}

func SignContract(ctx *astral.Context, contract *auth.Contract) (*auth.SignedContract, error) {
	return Default().SignContract(ctx, contract)
}
