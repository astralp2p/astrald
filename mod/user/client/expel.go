package user

import (
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Expel asks the target user node to permanently ban nodeID from the swarm and
// returns the signed ban.
func (client *Client) Expel(ctx *astral.Context, nodeID *astral.Identity) (signed *user.SignedExpulsion, err error) {
	ch, err := client.queryCh(ctx, user.OpExpel, query.Args{"target": nodeID.String()})
	if err != nil {
		return
	}
	defer ch.Close()

	err = ch.Switch(channel.Expect(&signed), channel.PassErrors)
	return
}
