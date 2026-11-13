package apphost

import (
	"fmt"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
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
// permits are written, and where. Its Vfs and Evaluator, both on when omitted,
// ask for a VFS of the app and for the app to be the evaluator of that VFS;
// the policy does not see them.
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

	req, err := receiveRegisterRequest(ch, query.QueryString())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: an op runs on a context detached from the requester's, and only the
	// conn tells a waiting decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetRegisterAppPolicy()(policyCtx, appRegisterRequest(req, origin))
	switch {
	case err != nil:
		mod.log.Logv(1, "registration refused: %v", err)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	case !bool(dec.Allow):
		mod.log.Logv(1, "registration from origin %v refused by policy", origin)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	}
	if err = validateRegisterDecision(dec); err != nil {
		mod.log.Logv(1, "registration refused: %v", err)
		return ch.Send(apphostapi.ErrRegistrationDeclined)
	}

	token, err := mod.provisionApp(ctx, dec)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.provisionAppVFS(req, token.Identity); err != nil {
		return ch.Send(astral.Err(err))
	}
	return ch.Send(token)
}

// validateRegisterDecision returns the first reason the node cannot apply dec
// to an apphost.register: an invalid authorizer assignment, or a contract
// permit for mod.auth.sudo_action.
//
// why sudo_action: the node signs every contract permit as issuer, and a
// node-signed mod.auth.sudo_action lets the app act and sign as the node
// (decided 2026-11-06).
// why not in provisionApp: sys.register_manager shares it through ProvisionApp,
// and the manager may hold any permit it asks for.
func validateRegisterDecision(dec *apphostapi.AppRegisterDecision) error {
	if err := dec.Validate(); err != nil {
		return err
	}
	sudo := auth.SudoAction{}.ObjectType()
	for _, p := range dec.ContractPermits {
		if p != nil && string(p.Action) == sudo {
			return fmt.Errorf("app register decision: a contract permit names %v", sudo)
		}
	}
	return nil
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

// receiveRegisterRequest refuses a removed argument in queryString and reads
// one mod.apphost.register_request from ch. A request with an invalid permit
// is refused before any policy is asked.
func receiveRegisterRequest(ch *channel.Channel, queryString string) (*apphostapi.RegisterRequest, error) {
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

	return req, nil
}

// appRegisterRequest is the question the register policy decides for req,
// with the origin the node read.
func appRegisterRequest(req *apphostapi.RegisterRequest, origin string) *apphostapi.AppRegisterRequest {
	return &apphostapi.AppRegisterRequest{
		Origin:          astral.String8(origin),
		Name:            req.Name,
		GrantPermits:    registerGrantRequest(req.GrantPermits),
		ContractPermits: req.ContractPermits,
	}
}

// provisionAppVFS creates the VFS of a new app when req asks for one, and
// names the app the evaluator of it when req asks for that too.
//
// why here: sys.register_manager shares provisionApp through ProvisionApp,
// and the manager app gets no VFS (decided 2026-11-12).
// why a direct call: the VFS belongs to the identity just made, and the
// call is not an authorized query (VFS 6, section 11).
// why no rollback: a failure after this step leaves an empty VFS, and a
// repeated registration is harmless because creating a VFS that exists
// succeeds.
// note: removing an app must remove its VFS; astrald has no way to remove
// an app yet.
func (mod *Module) provisionAppVFS(req *apphostapi.RegisterRequest, app *astral.Identity) error {
	if !req.RequestsVFS() {
		return nil
	}
	if err := mod.VFS.CreateVFS(app); err != nil {
		return err
	}
	if !req.RequestsEvaluator() {
		return nil
	}

	return mod.VFS.SetEvaluator(app, appEvaluatorEndpoint(app))
}

// appEvaluatorEndpoint is the endpoint at which an app evaluates access to
// its own VFS: astral://<app identity>:vfs.authorize.
func appEvaluatorEndpoint(app *astral.Identity) string {
	return "astral://" + app.String() + ":" + vfsapi.MethodAuthorize
}
