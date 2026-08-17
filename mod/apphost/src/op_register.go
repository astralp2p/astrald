package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/log/views"
)

const RegisterDuration = 10 * 365 * 24 * time.Hour

// A permit is the same clause on both rails; what the app names is the record
// it wants the permit written into. Both lists are comma-separated, and an
// action name carries no comma, so the joining is unambiguous. Asking is not
// receiving: the node's policy decides which permits are written, and where.
type opRegisterArgs struct {
	// GrantPermits asks the node to record these as node-local grants:
	// revocable by deleting a row, and worthless off this node.
	GrantPermits string

	// ContractPermits asks the node to record these in a signed node→app
	// contract: portable evidence another node verifies, durable until it
	// expires.
	ContractPermits string

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

	// why: the trusted-source template joins the contract request rather than
	// the grant one, because a PermitConfig carries Delegation and delegation
	// means nothing to a grant, which never leaves the node to be delegated.
	requestedGrantPermits := parsePermits(args.GrantPermits)
	requestedContractPermits := append(mod.GetWebOriginPermits(origin), parsePermits(args.ContractPermits)...)

	grantPermits, contractPermits, ok := mod.GetAppRegisterPolicy()(
		origin, requestedGrantPermits, requestedContractPermits,
	)
	if !ok {
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

	// both rails expire with the registration, so an identity's authority ages
	// with the token that reaches it
	expiresAt := time.Now().Add(RegisterDuration)

	// record the grant permits on this node. A write failure sends the error
	// rather than a token, because a token for an identity holding less than
	// the policy allowed is a registration the app cannot detect went wrong.
	for _, permit := range grantPermits {
		if err = mod.Grant(guestID, permit, &expiresAt); err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	// render the contract permits as a node→app contract, so the app's
	// authority for those actions chains back through the node
	if len(contractPermits) > 0 {
		signedPermits := &auth.SignedContract{Contract: &auth.Contract{
			Issuer:    mod.node.Identity(),
			Subject:   guestID,
			Permits:   contractPermits,
			ExpiresAt: astral.Time(expiresAt),
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
