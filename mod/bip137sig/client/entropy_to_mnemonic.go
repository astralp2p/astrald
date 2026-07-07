package bip137sig

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

func (client *Client) EntropyToMnemonic(ctx *astral.Context, entropy *bip137sig.Entropy) (mnemonic []string, err error) {
	ch, err := client.queryCh(ctx, bip137sig.MethodMnemonic, nil)
	if err != nil {
		return
	}
	defer ch.Close()
	if err = ch.Send(entropy); err != nil {
		return
	}
	var mnemonicStr16 *astral.String16
	if err = ch.Switch(channel.Expect(&mnemonicStr16), channel.PassErrors); err != nil {
		return
	}
	return strings.Fields(mnemonicStr16.String()), nil
}
