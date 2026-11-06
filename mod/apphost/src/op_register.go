package apphost

import (
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/log/views"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

const RegisterDuration = 10 * 365 * 24 * time.Hour

type opRegisterArgs struct {
	In  string
	Out string
}

// OpRegister provisions a brand-new identity: generates a key pair, issues a signed app contract, and returns an access token.
// The caller receives a ready-to-use guest identity without providing any pre-existing credentials.
//
// The op reads one mod.apphost.register_request after accept. Its Name is the
// name the app gives itself, shown to a delegated policy and not verified by
// the node. Its GrantPermits ask the node to record node-local grants:
// revocable by deleting a row, and worthless off this node. Its
// ContractPermits ask the node to record a signed node→app contract: portable
// evidence another node verifies, durable until it expires. Both lists carry
// their constraints. Asking is not receiving: the node's policy decides which
// permits are written, and where.
//
// why: no named action guards this op; the app register policy decides every
// registration, and the bundled policy accepts every one until a delegate is
// set (decided 2026-10-28).
func (mod *Module) OpRegister(ctx *astral.Context, query *routing.IncomingQuery, args opRegisterArgs) (err error) {
	// why: read the registering web origin before accepting - EnRouteQueryExtras
	// only resolves while the query is en route, and Accept removes that entry.
	extras := mod.EnRouteQueryExtras(query.Nonce())
	origin, _ := extras[apphost.ExtraOriginWeb].(string)

	// why accept before deciding: a delegate may wait on a person, and a query
	// still en route is bounded by the router's timeout.
	conn := query.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: identities minted over a link are indistinguishable from local apps.
	if query.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(query.Origin()))
	}

	req, err := receiveAppRegisterRequest(ch, query.QueryString(), origin)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: an op runs on a context detached from the requester's, and only the
	// conn tells a waiting decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetRegisterAppPolicy()(policyCtx, req)
	switch {
	case err != nil:
		mod.log.Logv(1, "registration refused: %v", err)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	case !bool(dec.Allow):
		mod.log.Logv(1, "registration from origin %v refused by policy", origin)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	}
	if err = dec.Validate(); err != nil {
		mod.log.Logv(1, "registration refused: %v", err)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	}

	token, err := mod.provisionApp(ctx, dec)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	return ch.Send(token)
}

// provisionApp creates an App identity: a key pair, a signed app contract and
// the permits and authorizers dec lists. It returns the access token of the
// new identity.
//
// A write failure returns the error rather than a token, because a token for
// an identity holding less than the policy allowed is a registration the app
// cannot detect went wrong.
func (mod *Module) provisionApp(ctx *astral.Context, dec *apphostapi.AppRegisterDecision) (*apphostapi.AccessToken, error) {
	key := secp256k1.New()
	guestID := secp256k1.Identity(secp256k1.PublicKey(key))

	// why: every app receives the relay contract, whatever the policy decided;
	// the contract is part of registration (decided 2026-11-06).
	signed, err := mod.storeAppContract(ctx, key, guestID)
	if err != nil {
		return nil, err
	}

	if err = mod.writeAppAuthority(ctx, guestID, dec); err != nil {
		return nil, err
	}

	// generate an access token for the guest
	token, err := mod.CreateAccessToken(guestID, astral.Duration(RegisterDuration))
	if err != nil {
		return nil, err
	}

	// why: a sibling routes a query to the app only through a relay-for contract
	// in its index, and the first-link sync has already run for every linked sibling.
	// why after the token: a registration that fails leaves the swarm no
	// contract of an app nobody holds a token for.
	// note: the push is best-effort; a sibling that misses it gets the contract at its next first link.
	if mod.User != nil {
		mod.User.PushToSiblings(mod.ctx, signed)
	}

	tv := views.NewTimeView(&token.ExpiresAt)
	tv.Layout = views.LongTimeLayout

	mod.log.Logv(1, "registered guest %v until %v", token.Identity, tv)
	return token, nil
}

// storeAppContract signs the relay contract between guestID and the node as
// both parties, indexes it, and stores it and key.
func (mod *Module) storeAppContract(ctx *astral.Context, key *crypto.PrivateKey, guestID *astral.Identity) (*auth.SignedContract, error) {
	contract, err := apphost.NewAppContract(guestID, mod.node.Identity(), RegisterDuration)
	if err != nil {
		return nil, err
	}

	signed := &auth.SignedContract{Contract: contract}
	if signed.IssuerSig, err = secp256k1sig.SignASN1(key, contract.SignableHash()); err != nil {
		return nil, err
	}
	if _, err = mod.Auth.SignSubject(ctx, signed); err != nil {
		return nil, err
	}
	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return nil, err
	}
	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), key); err != nil {
		return nil, err
	}
	if err = mod.Crypto.AddToIndex(key); err != nil {
		return nil, err
	}
	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return nil, err
	}
	return signed, nil
}

// writeAppAuthority records the grant permits of dec on this node, renders its
// contract permits as a node→app contract, and records its authorizer
// assignments with guestID as actor.
//
// why both rails expire with the registration: an identity's authority ages
// with the token that reaches it.
// why the authorizers last, before the token: a token for an identity whose
// authorizer is not in place is a registration the app cannot detect went
// wrong.
func (mod *Module) writeAppAuthority(ctx *astral.Context, guestID *astral.Identity, dec *apphostapi.AppRegisterDecision) error {
	expiresAt := time.Now().UTC().Add(RegisterDuration)

	for _, permit := range dec.GrantPermits {
		if err := mod.Grant(guestID, permit, &expiresAt); err != nil {
			return err
		}
	}

	if len(dec.ContractPermits) > 0 {
		if err := mod.storePermitContract(ctx, guestID, dec.ContractPermits, expiresAt); err != nil {
			return err
		}
	}

	for _, a := range dec.Authorizers {
		assignment := *a
		assignment.Actor = guestID
		if err := mod.Auth.SetAuthorizer(ctx, &assignment); err != nil {
			return err
		}
	}
	return nil
}

// storePermitContract signs, indexes and stores a node→app contract carrying
// permits, so the app's authority for those actions chains back through the
// node.
func (mod *Module) storePermitContract(ctx *astral.Context, guestID *astral.Identity, permits []*auth.Permit, expiresAt time.Time) error {
	signed := &auth.SignedContract{Contract: &auth.Contract{
		Issuer:    mod.node.Identity(),
		Subject:   guestID,
		Permits:   permits,
		ExpiresAt: astral.Time(expiresAt),
	}}

	if err := mod.Auth.SignContract(ctx, signed); err != nil {
		return err
	}
	if err := mod.Auth.IndexContract(ctx, signed); err != nil {
		return err
	}
	_, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	return err
}

// receiveAppRegisterRequest refuses a removed argument in queryString, reads
// one mod.apphost.register_request from ch, and returns the question the
// register policy decides, with the origin the node read. A request with an
// invalid permit is refused before any policy is asked.
func receiveAppRegisterRequest(ch *channel.Channel, queryString, origin string) (*apphostapi.AppRegisterRequest, error) {
	if err := refuseRemovedArgs(queryString, "name", "grant_permits", "contract_permits"); err != nil {
		return nil, err
	}

	var req *apphostapi.RegisterRequest
	if err := ch.Switch(channel.Expect(&req), channel.FailOnEOF()); err != nil {
		return nil, err
	}
	if err := validatePermits(req.GrantPermits); err != nil {
		return nil, err
	}
	if err := validatePermits(req.ContractPermits); err != nil {
		return nil, err
	}

	return &apphostapi.AppRegisterRequest{
		Origin:          astral.String8(origin),
		Name:            req.Name,
		GrantPermits:    registerGrantRequest(req.GrantPermits),
		ContractPermits: req.ContractPermits,
	}, nil
}
