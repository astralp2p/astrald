package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/user"
)

// NewContract requests a new node-binding contract for the user identified by alias from the remote user module.
func (client *Client) NewContract(ctx *astral.Context, alias string) (contract *auth.Contract, err error) {
	ch, err := client.queryCh(ctx, user.OpNewNodeContract, query.Args{"user": alias})
	if err != nil {
		return
	}
	defer ch.Close()
	err = ch.Switch(channel.Expect(&contract), channel.PassErrors)
	return
}
