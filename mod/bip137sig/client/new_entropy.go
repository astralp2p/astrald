package bip137sig

import (
	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) NewEntropy(ctx *astral.Context, bits int) (entropy *bip137sig.Entropy, err error) {
	ch, err := client.queryCh(ctx, bip137sig.MethodNewEntropy, query.Args{"bits": bits})
	if err != nil {
		return
	}
	defer ch.Close()
	err = ch.Switch(channel.Expect(&entropy), channel.PassErrors)

	return
}
