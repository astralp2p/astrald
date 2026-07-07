package bip137sig

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/bip137sig"
)

func (client *Client) DeriveKey(ctx *astral.Context, path string, seed *bip137sig.Seed) (privateKey *crypto.PrivateKey, err error) {
	ch, err := client.queryCh(ctx, bip137sig.MethodDeriveKey, query.Args{"path": path})
	if err != nil {
		return
	}
	defer ch.Close()
	if err = ch.Send(seed); err != nil {
		return
	}
	err = ch.Switch(channel.Expect(&privateKey), channel.PassErrors)
	return
}
