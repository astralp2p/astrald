package bip137sig

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) MnemonicToSeed(ctx *astral.Context, mnemonic []string, passphrase string) (seed *bip137sig.Seed, err error) {
	ch, err := client.queryCh(ctx, bip137sig.MethodSeed, query.Args{"passphrase": passphrase})
	if err != nil {
		return
	}
	defer ch.Close()
	mnemonicStr16 := astral.String16(strings.Join(mnemonic, " "))
	if err = ch.Send(&mnemonicStr16); err != nil {
		return
	}
	err = ch.Switch(channel.Expect(&seed), channel.PassErrors)
	return
}
