package crypto

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/mod/crypto"
)

func (client *Client) PublicKey(ctx *astral.Context, privateKey *crypto.PrivateKey) (publicKey *crypto.PublicKey, err error) {
	ch, err := client.queryCh(ctx, crypto.MethodPublicKey, nil)
	if err != nil {
		return
	}
	defer ch.Close()
	if err = ch.Send(privateKey); err != nil {
		return
	}
	err = ch.Switch(channel.Expect(&publicKey), channel.PassErrors)
	return
}
