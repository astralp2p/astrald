package user

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// AcceptMembership submits a signed contract to the remote user node and returns the subject's countersignature.
// The caller must supply the issuer's signature; the remote signs back as the subject.
func (client *Client) AcceptMembership(ctx *astral.Context, contract *auth.Contract, issuerSig *crypto.Signature) (subjectSig *crypto.Signature, err error) {
	ch, err := client.queryCh(ctx, user.OpAcceptMembership, nil)
	if err != nil {
		return
	}
	defer ch.Close()

	err = ch.Send(contract)
	if err != nil {
		return
	}

	err = ch.Send(issuerSig)
	if err != nil {
		return
	}

	err = ch.Switch(channel.Expect(&subjectSig), channel.PassErrors)
	return
}
