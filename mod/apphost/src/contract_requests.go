package apphost

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// Rate limits of apphost.request_contract, counted over
// ContractRequestWindow on the node that files the requests. A call answered
// with a live request it already filed does not count.
//
// why these numbers: a request stays answerable for an hour, and a second
// call with the same permits returns the live request, so an app files more
// than one request an hour only after a Decline, an expiry or a change of
// permits. The node-wide limit bounds what all apps on a node add to the
// grow-only SwarmState that every member syncs and shows.
const (
	ContractRequestWindow    = user.RequestTimeout
	ContractRequestsPerApp   = 4
	ContractRequestsPerNode  = 64
	contractRequestPollEvery = 5 * time.Second
)

// AppContractDuration is how long a contract the host signs for its app holds
// at most. It never outlives the approver's contract to the host, nor the
// host's own membership.
const AppContractDuration = RegisterDuration

// contractSigner signs a contract as its issuer and, when it holds the key,
// its subject (SignContract), or as its subject alone (SignSubject);
// auth.Module satisfies it.
type contractSigner interface {
	SignContract(ctx *astral.Context, contract *auth.SignedContract) error
	SignSubject(ctx *astral.Context, contract *auth.SignedContract) (*crypto.Signature, error)
}

// requestVerifier verifies a signature; crypto.Module satisfies it.
type requestVerifier interface {
	Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error
}

// contractRequests files the ContractRequests of the apps this node hosts,
// co-signs an approver's offer for each, and signs the app's contract (the
// host worker). The grant is two contracts: the approver's to this node,
// with one more Delegation, and this node's to the app
// (astral-docs protocols/user/README.md#app-grants). It writes SwarmState
// only through the typed writes of user.SwarmState, which sign the request as
// this node, and reads what entries mean only through user.SwarmView.
type contractRequests struct {
	node    *astral.Identity
	state   usermod.SwarmState
	view    usermod.SwarmView
	signer  contractSigner
	now     func() time.Time
	limiter *requestLimiter
	// verifier checks this node's own signature on a request read from
	// SwarmState; nil: only the requests this process filed are its own.
	verifier requestVerifier
	// root returns the root key, whose offer is preferred; nil prefers none.
	root func() *astral.Identity

	// mu makes finding a live request and filing a new one one step, so two
	// calls of one app never file two requests.
	mu sync.Mutex

	// filed holds the requests this process filed, by entry ID, so a request
	// SwarmState holds (ErrSwarmEntryHeld) is still found and waited on.
	filed sig.Map[string, *user.SignedContractRequest]
	// cosigned holds the offers this process co-signed, by the entry ID of
	// their request, so a request gets one co-signed offer even before
	// SwarmState stores it.
	cosigned sig.Map[string, *auth.SignedContract]
	// signed holds the app contracts this process signed, by the entry ID of
	// the request they carry out, so each is signed once even before
	// SwarmState stores it.
	signed sig.Map[string, *auth.SignedContract]
	// changed is closed and replaced after every poll; Wait sleeps on it.
	changed atomic.Pointer[chan struct{}]
}

func newContractRequests(node *astral.Identity, state usermod.SwarmState, view usermod.SwarmView,
	signer contractSigner) *contractRequests {
	cr := &contractRequests{
		node:    node,
		state:   state,
		view:    view,
		signer:  signer,
		now:     time.Now,
		limiter: newRequestLimiter(ContractRequestWindow, ContractRequestsPerApp, ContractRequestsPerNode),
	}
	changed := make(chan struct{})
	cr.changed.Store(&changed)
	return cr
}

// File returns the entry ID of the ContractRequest that asks permits for app.
// A live request this node filed for app with the same permits (pending or
// offered and unexpired, or fulfilled) is returned as is; otherwise File
// files a new request in SwarmState, signed by this node, with the default
// one-hour Timeout. A new request counts against the rate limits; a refused
// one is ErrContractRequestRateLimited.
func (cr *contractRequests) File(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*astral.ObjectID, error) {
	switch {
	case app.IsZero():
		return nil, apphostmod.ErrMissingAppIdentity
	case len(permits) == 0:
		return nil, errors.New("missing permits")
	}
	if err := validatePermits(permits); err != nil {
		return nil, err
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	if id := cr.findLive(app, permits); id != nil {
		return id, nil
	}

	if !cr.limiter.allow(app, cr.now()) {
		return nil, apphostmod.ErrContractRequestRateLimited
	}

	// note: a held request counts as filed: SwarmState admits it once this
	// node's membership arrives, and filing again would add a second one.
	signed, err := cr.state.FileContractRequest(ctx, app, permits)
	if err != nil {
		return nil, fmt.Errorf("file contract request: %w", err)
	}
	id, err := astral.ResolveObjectID(signed)
	if err != nil {
		return nil, err
	}

	cr.filed.Replace(id.String(), signed)
	return id, nil
}

// findLive returns the entry ID of a live request this node filed for app
// asking exactly permits, or nil.
//
// why SwarmState and not only memory: a request outlives a restart of the
// host, and filing it again after one would ask the user twice.
func (cr *contractRequests) findLive(app *astral.Identity, permits []*auth.Permit) *astral.ObjectID {
	asked := &user.ContractRequest{Permits: permits}
	for _, c := range cr.ownRequests() {
		r := c.req.ContractRequest
		if !r.Identity.IsEqual(app) || !samePermits(r, asked) {
			continue
		}
		switch cr.view.RequestStatus(c.id) {
		case user.RequestFulfilled:
			return c.id
		case user.RequestPending, user.RequestOffered:
			if !r.Expired(cr.now()) {
				return c.id
			}
		}
	}
	return nil
}

type ownRequest struct {
	id  *astral.ObjectID
	req *user.SignedContractRequest
}

// ownRequests returns the ContractRequests this node filed: those SwarmState
// stores and those this process filed, once each.
func (cr *contractRequests) ownRequests() []ownRequest {
	seen := map[string]bool{}
	var list []ownRequest

	ids, _ := cr.state.SwarmEntries(user.EntryRequests)
	for _, id := range ids {
		if req := cr.ownRequest(id); req != nil {
			seen[id.String()] = true
			list = append(list, ownRequest{id, req})
		}
	}

	for key, req := range cr.filed.Clone() {
		if seen[key] {
			continue
		}
		if id, err := astral.ResolveObjectID(req); err == nil {
			list = append(list, ownRequest{id, req})
		}
	}
	return list
}

// ownRequest returns the ContractRequest with entry ID id if it is in this
// node's own records, or nil: this process filed it, or SwarmState holds it
// with this node as FiledBy and this node's signature, which this node
// verifies itself.
//
// why verify here: this node co-signs only what it asked for, and its
// consent is its own signature on the request, not SwarmState's merge check.
func (cr *contractRequests) ownRequest(id *astral.ObjectID) *user.SignedContractRequest {
	if req, ok := cr.filed.Get(id.String()); ok {
		return req
	}

	obj, err := cr.state.SwarmEntry(id)
	if err != nil || cr.verifier == nil {
		return nil
	}
	req, ok := obj.(*user.SignedContractRequest)
	if !ok || req.IsNil() || !req.FiledBy.IsEqual(cr.node) || req.Identity.IsZero() || req.FiledBySig == nil {
		return nil
	}
	if cr.verifier.Verify(secp256k1.FromIdentity(cr.node), req.FiledBySig, req.ContractRequest) != nil {
		return nil
	}
	return req
}

// Poll carries out the requests this node filed. For each one it co-signs at
// most one approver's offer (cosign), which completes the approver's
// contract to this node, and then signs, once, the app's contract from that
// contract (carryOut), adding both to SwarmState. It then wakes every Wait.
//
// A request another node filed is that node's to carry out, even when it
// names an app hosted here: an app moved here files its own request.
func (cr *contractRequests) Poll(ctx *astral.Context) error {
	defer cr.notify()

	var errs []error
	all := cr.ownRequests()
	for _, own := range all {
		if err := cr.pollRequest(ctx, own, all); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// pollRequest co-signs an offer for own when none is co-signed yet, and signs
// the app's contract from the co-signed one when it is not signed yet. all
// are this node's own requests.
func (cr *contractRequests) pollRequest(ctx *astral.Context, own ownRequest, all []ownRequest) error {
	if cr.carriedOut(own.id, own.req) != nil {
		return nil
	}
	granted := cr.granted(own.id, own.req)
	if granted == nil {
		offer := cr.pickOffer(own, all)
		if offer == nil {
			return nil
		}
		var err error
		if granted, err = cr.cosign(ctx, own.id, offer); err != nil {
			return err
		}
	}
	return cr.carryOut(ctx, own, granted)
}

// pickOffer returns the offer this node co-signs for own, or nil. The request
// must be open (openToOffers, against all of this node's requests); an offer qualifies when only its issuer signed
// it, it is given to this node, its permits follow the offer depth rule
// (user.ContractRequest.CoversOffer), it has not expired, and its issuer may
// answer the request. Of several it takes the root key's, then the one that
// expires last, then the one with the lowest entry ID.
// Spec: .ai/system/protocols/user/README.md#app-grants
func (cr *contractRequests) pickOffer(own ownRequest, all []ownRequest) *auth.SignedContract {
	if !cr.openToOffers(own, all) {
		return nil
	}
	var best *auth.SignedContract
	var bestID string
	for _, c := range cr.view.ContractsOf(own.id) {
		if c.IsNil() || c.IssuerSig == nil || c.SubjectSig != nil || !c.Subject.IsEqual(cr.node) ||
			!c.RequestID.IsEqual(own.id) || !own.req.CoversOffer(c.Contract) ||
			!cr.now().Before(c.ExpiresAt.Time()) || !cr.view.MayAnswer(c.Issuer, own.id) {
			continue
		}
		id, err := astral.ResolveObjectID(c)
		if err != nil {
			continue
		}
		if best == nil || cr.prefers(c, id.String(), best, bestID) {
			best, bestID = c, id.String()
		}
	}
	return best
}

// prefers reports whether offer a, with entry ID aID, comes before offer b:
// the root key's first, then the later expiry, then the lower entry ID.
func (cr *contractRequests) prefers(a *auth.SignedContract, aID string, b *auth.SignedContract, bID string) bool {
	var root *astral.Identity
	if cr.root != nil {
		root = cr.root()
	}
	if ra, rb := !root.IsZero() && a.Issuer.IsEqual(root), !root.IsZero() && b.Issuer.IsEqual(root); ra != rb {
		return ra
	}
	if ta, tb := a.ExpiresAt.Time(), b.ExpiresAt.Time(); !ta.Equal(tb) {
		return ta.After(tb)
	}
	return aID < bID
}

// openToOffers reports whether this node still co-signs an offer for own: the
// request is pending or offered, its own copy has not expired, and no later
// request among all, this node's own, for the same identity with the same set
// of permits supersedes it.
func (cr *contractRequests) openToOffers(own ownRequest, all []ownRequest) bool {
	switch cr.view.RequestStatus(own.id) {
	case user.RequestPending, user.RequestOffered:
	default:
		return false
	}
	if own.req.Expired(cr.now()) {
		return false
	}
	for _, other := range all {
		r := other.req.ContractRequest
		if r.Identity.IsEqual(own.req.Identity) && samePermits(r, own.req.ContractRequest) &&
			r.FiledAt.Time().After(own.req.FiledAt.Time()) {
			return false
		}
	}
	return true
}

// cosign adds this node's signature to offer, which completes the approver's
// contract to this node for the request reqID, and adds it to SwarmState.
//
// why no other decision: this node's consent is the request it signed, and
// the approver's decision is its signature.
func (cr *contractRequests) cosign(ctx *astral.Context, reqID *astral.ObjectID, offer *auth.SignedContract) (*auth.SignedContract, error) {
	done := &auth.SignedContract{Contract: offer.Contract, IssuerSig: offer.IssuerSig}
	if _, err := cr.signer.SignSubject(ctx, done); err != nil {
		return nil, fmt.Errorf("co-sign the offer of %v: %w", offer.Issuer, err)
	}
	if err := cr.state.AddContract(ctx, done); err != nil {
		return nil, fmt.Errorf("add the contract from %v: %w", offer.Issuer, err)
	}
	cr.cosigned.Replace(reqID.String(), done)
	return done, nil
}

// granted returns the approver's contract to this node for the request reqID
// that this node co-signed, or nil.
func (cr *contractRequests) granted(reqID *astral.ObjectID, req *user.SignedContractRequest) *auth.SignedContract {
	if c, ok := cr.cosigned.Get(reqID.String()); ok {
		return c
	}
	for _, c := range cr.view.ContractsOf(reqID) {
		if !c.IsNil() && c.IssuerSig != nil && c.SubjectSig != nil && c.Subject.IsEqual(cr.node) &&
			req.CoversOffer(c.Contract) {
			return c
		}
	}
	return nil
}

// carryOut signs the app's contract from granted, the approver's contract to
// this node, and adds it to SwarmState. It passes the permits on only to the
// app the request names: each offered permit one Delegation lower, which is
// the requested permit.
func (cr *contractRequests) carryOut(ctx *astral.Context, own ownRequest, granted *auth.SignedContract) error {
	permits := make([]*auth.Permit, 0, len(granted.Permits))
	for _, p := range granted.Permits {
		onward := *p
		onward.Delegation--
		permits = append(permits, &onward)
	}
	signed := &auth.SignedContract{Contract: &auth.Contract{
		Issuer:    cr.node,
		Subject:   own.req.Identity,
		Permits:   permits,
		ExpiresAt: astral.Time(cr.contractExpiry(granted.ExpiresAt.Time())),
		RequestID: own.id,
	}}
	if err := cr.signer.SignContract(ctx, signed); err != nil {
		return fmt.Errorf("sign contract for %v: %w", own.req.Identity, err)
	}
	if err := cr.state.AddContract(ctx, signed); err != nil {
		return fmt.Errorf("add contract for %v: %w", own.req.Identity, err)
	}

	cr.signed.Replace(own.id.String(), signed)
	return nil
}

// contractExpiry returns when an app contract signed now expires: after
// AppContractDuration, at granted, the expiry of the approver's contract to
// this node, or when this node's membership expires, whichever is earliest.
//
// why: a link never outlives its parent.
func (cr *contractRequests) contractExpiry(granted time.Time) time.Time {
	expiresAt := cr.now().Add(AppContractDuration)
	if granted.Before(expiresAt) {
		expiresAt = granted
	}
	if m, ok := cr.view.Membership(cr.node); ok && m.SignedContract != nil && m.Contract != nil {
		if t := m.ExpiresAt.Time(); t.Before(expiresAt) {
			expiresAt = t
		}
	}
	return expiresAt
}

// carriedOut returns the contract this node signed for the request with entry
// ID reqID, or nil.
func (cr *contractRequests) carriedOut(reqID *astral.ObjectID, req *user.SignedContractRequest) *auth.SignedContract {
	if c, ok := cr.signed.Get(reqID.String()); ok {
		return c
	}
	for _, c := range cr.view.ContractsOf(reqID) {
		if c != nil && c.Contract != nil && c.SubjectSig != nil && c.Issuer.IsEqual(cr.node) &&
			c.Subject.IsEqual(req.Identity) && req.Covers(c.Contract) {
			return c
		}
	}
	return nil
}

// Outcome returns the result of the request with entry ID reqID this node
// filed: the contract carrying it out, ErrContractDeclined, or
// ErrContractRequestExpired. Both are nil while the request is pending or
// offered, or fulfilled before the app's contract is signed.
//
// why a fulfilled request wins over a Decline: a Decline only closes its
// request, and a contract both parties signed keeps its authority (PR 4
// decision Q1).
func (cr *contractRequests) Outcome(reqID *astral.ObjectID) (*auth.SignedContract, error) {
	req := cr.ownRequest(reqID)
	if req == nil {
		return nil, fmt.Errorf("no contract request %v filed by this node", reqID)
	}
	if c := cr.carriedOut(reqID, req); c != nil {
		return c, nil
	}
	switch cr.view.RequestStatus(reqID) {
	case user.RequestFulfilled:
		return nil, nil
	case user.RequestDeclined:
		return nil, apphostmod.ErrContractDeclined
	case user.RequestExpired:
		return nil, apphostmod.ErrContractRequestExpired
	}
	// why its own copy: a request SwarmState holds reads as pending.
	if req.Expired(cr.now()) {
		return nil, apphostmod.ErrContractRequestExpired
	}
	return nil, nil
}

// Wait blocks until the request with entry ID reqID has an outcome (see
// Outcome) or ctx ends. It checks again after every poll.
func (cr *contractRequests) Wait(ctx *astral.Context, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	for {
		changed := cr.changes()
		c, err := cr.Outcome(reqID)
		if c != nil || err != nil {
			return c, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (cr *contractRequests) changes() <-chan struct{} {
	return *cr.changed.Load()
}

// notify wakes every Wait.
func (cr *contractRequests) notify() {
	next := make(chan struct{})
	close(*cr.changed.Swap(&next))
}

// Run is the host worker: it polls SwarmState's head every interval, polls
// for offers when the head moved, and wakes every Wait on each tick, so a
// request that expires reaches its app without a new entry. It returns when
// ctx ends.
//
// note: switch to vfs.watch once it lands; only this loop changes.
func (cr *contractRequests) Run(ctx *astral.Context, interval time.Duration, logErr func(error)) {
	if interval <= 0 {
		interval = contractRequestPollEvery
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var head uint64
	first := true
	for {
		if h := cr.state.SwarmHead(); first || h != head {
			first, head = false, h
			if err := cr.Poll(ctx); err != nil && logErr != nil {
				logErr(err)
			}
		} else {
			cr.notify()
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// samePermits reports whether a and b ask for the same set of permits.
func samePermits(a, b *user.ContractRequest) bool {
	return a.Covers(&auth.Contract{Permits: b.Permits}) && b.Covers(&auth.Contract{Permits: a.Permits})
}

// requestLimiter counts the requests a node files over a sliding window, per
// app and for the whole node.
type requestLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	perApp  int
	perNode int
	byApp   map[string][]time.Time
	all     []time.Time
}

func newRequestLimiter(window time.Duration, perApp, perNode int) *requestLimiter {
	return &requestLimiter{window: window, perApp: perApp, perNode: perNode, byApp: map[string][]time.Time{}}
}

// allow records a request by app at now and reports true, or reports false
// and records nothing when app or the node is at its limit.
func (l *requestLimiter) allow(app *astral.Identity, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	recent := func(ts []time.Time) []time.Time {
		return slices.DeleteFunc(ts, func(t time.Time) bool { return !t.After(cutoff) })
	}
	key := app.String()
	l.all = recent(l.all)
	mine := recent(l.byApp[key])
	if len(mine) == 0 {
		delete(l.byApp, key)
	}

	if len(mine) >= l.perApp || len(l.all) >= l.perNode {
		if len(mine) > 0 {
			l.byApp[key] = mine
		}
		return false
	}
	l.byApp[key] = append(mine, now)
	l.all = append(l.all, now)
	return true
}
