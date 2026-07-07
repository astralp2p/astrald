package secp256k1

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/secp256k1"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

func (c *Client) NewKey(ctx *astral.Context) (key *crypto.PrivateKey, err error) {
	ch, err := c.queryCh(ctx, secp256k1.MethodNew, nil)
	if err != nil {
		return
	}
	defer ch.Close()

	err = ch.Switch(channel.Expect(&key), channel.PassErrors)
	return
}

func NewKey(ctx *astral.Context) (*crypto.PrivateKey, error) {
	return Default().NewKey(ctx)
}
