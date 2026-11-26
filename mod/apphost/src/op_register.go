package apphost

import (
	"fmt"
	"slices"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
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
// name the app gives itself and is not verified by the node. Its
// GrantPermits and ContractPermits, with their constraints, are the permits
// the app asks for: the node files one ContractRequest for both lists in
// SwarmState, and signs the app's contract once an Approval answers it
// (apphost.request_contract waits for that). Asking is not receiving. Its Vfs
// and Evaluator, both on when omitted, ask for a VFS of the app and for the
// app to be the evaluator of that VFS.
//
// why: no named action guards this op; registration stays local and issues
// the identity and token at once (vision, 2026-11-25).
func (mod *Module) OpRegister(ctx *astral.Context, query *routing.IncomingQuery, args opRegisterArgs) (err error) {
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

	token, err := mod.provisionApp(ctx, nil)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.fileRegisterPermits(ctx, token.Identity, req); err != nil {
		mod.log.Logv(1, "registration of %v: contract request: %v", token.Identity, err)
	}

	if err = mod.provisionAppVFS(req, token.Identity); err != nil {
		return ch.Send(astral.Err(err))
	}
	return ch.Send(token)
}

// fileRegisterPermits files a ContractRequest for the permits a registration
// asks, so the app holds them once an Approval is carried out. The app waits
// for the outcome with apphost.request_contract and the same permits.
//
// why no decision here: registration stays local and is never refused; only
// the permits go through SwarmState (vision, 2026-11-25).
//
// why the hosting permits first: a registered app is served from this node
// and syncs the blueprints of its types (apps.WithBlueprintSync), so every
// registration asks for them.
func (mod *Module) fileRegisterPermits(ctx *astral.Context, app *astral.Identity, req *apphostapi.RegisterRequest) error {
	if mod.contractRequests == nil {
		return nil
	}
	permits := slices.Concat(hostingPermits(), req.GrantPermits, req.ContractPermits)
	_, err := mod.contractRequests.File(ctx, app, permits)
	return err
}

// refuseRegisterPermits refuses the permits no app is ever approved for.
//
// why sudo_action: a contract permit for mod.auth.sudo_action lets the app act
// and sign as its host (decided 2026-11-06).
// why relay_for_action: the host→app contract names the app as subject, which
// is no swarm member, so the relay admission rule refuses to index it
// (astral-docs topics/app-routing.md, Reach).
func refuseRegisterPermits(permits []*auth.Permit) error {
	refused := []string{auth.SudoAction{}.ObjectType(), nodes.RelayForAction{}.ObjectType()}
	for _, p := range permits {
		if p != nil && slices.Contains(refused, string(p.Action)) {
			return fmt.Errorf("a registration names %v", p.Action)
		}
	}
	return nil
}

// provisionApp creates an App identity: a key pair, a signed app contract
// and, for sys.register_manager on a fresh node, a node→app contract holding
// contractPermits. It returns the access token of the new identity.
//
// A write failure returns the error rather than a token, because a token for
// an identity holding less than it asked for is a registration the app
// cannot detect went wrong.
func (mod *Module) provisionApp(ctx *astral.Context, contractPermits []*auth.Permit) (*apphostapi.AccessToken, error) {
	key := secp256k1.New()
	guestID := secp256k1.Identity(secp256k1.PublicKey(key))

	// why: every app receives the relay contract; the contract is part of
	// registration (decided 2026-11-06).
	signed, err := mod.storeAppContract(ctx, key, guestID)
	if err != nil {
		return nil, err
	}

	if len(contractPermits) > 0 {
		expiresAt := time.Now().UTC().Add(RegisterDuration)
		if err = mod.storePermitContract(ctx, guestID, contractPermits, expiresAt); err != nil {
			return nil, err
		}
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
	// note: the push retries a bounded number of times; a sibling that still misses
	// it gets the contract at its next link or at the periodic sibling sync (mod/user).
	if mod.User != nil && !mod.config.SkipSiblingPush {
		mod.User.PushToSiblingsRetry(mod.ctx, signed)
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
// is refused before anything is provisioned.
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
	if err := refuseRegisterPermits(slices.Concat(req.GrantPermits, req.ContractPermits)); err != nil {
		return nil, err
	}

	return req, nil
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
