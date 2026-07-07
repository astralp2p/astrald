package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/apphost"
	"github.com/cryptopunkscc/astrald/mod/log/views"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

const RegisterDuration = 10 * 365 * 24 * time.Hour

type opRegisterArgs struct {
	In  string
	Out string
}

// OpRegister provisions a brand-new identity: generates a key pair, issues a signed app contract, and returns an access token.
// The caller receives a ready-to-use guest identity without providing any pre-existing credentials.
func (mod *Module) OpRegister(ctx *astral.Context, query *routing.IncomingQuery, args opRegisterArgs) (err error) {
	// why: read the registering web origin before accepting - EnRouteQueryExtras
	// only resolves while the query is en route, and Accept removes that entry.
	extras := mod.EnRouteQueryExtras(query.Nonce())
	origin, _ := extras[apphost.ExtraOriginWeb].(string)

	// a trusted web source adds its permit template to the registration
	permits := mod.GetWebOriginPermits(origin)

	if !mod.GetAppRegisterPolicy()(origin, permits) {
		return query.RejectWithCode(1)
	}

	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// generate and store new private key
	key := secp256k1.New()
	_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), key)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.Crypto.AddToIndex(key)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	guestID := secp256k1.Identity(secp256k1.PublicKey(key))

	// generate and sign a contract for the guest
	contract, err := apphost.NewAppContract(guestID, mod.node.Identity(), RegisterDuration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	signed := &auth.SignedContract{Contract: contract}
	if err = mod.Auth.SignContract(ctx, signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	contractID, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// render the trusted-source permits as a node→app contract, so the app's
	// authority for the permitted actions chains back through the node
	if len(permits) > 0 {
		signedPermits := &auth.SignedContract{Contract: &auth.Contract{
			Issuer:    mod.node.Identity(),
			Subject:   guestID,
			Permits:   permits,
			ExpiresAt: astral.Time(time.Now().Add(RegisterDuration)),
		}}

		if err = mod.Auth.SignContract(ctx, signedPermits); err != nil {
			return ch.Send(astral.Err(err))
		}

		if err = mod.Auth.IndexContract(ctx, signedPermits); err != nil {
			return ch.Send(astral.Err(err))
		}

		if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signedPermits); err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	// generate an access token for the guest
	token, err := mod.CreateAccessToken(guestID, astral.Duration(RegisterDuration))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	tv := views.NewTimeView(&token.ExpiresAt)
	tv.Layout = views.LongTimeLayout

	mod.log.Logv(1, "registered guest %v until %v (%v)", token.Identity, tv, contractID)
	return ch.Send(token)
}
