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
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
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
// at most. It never outlives the host's own membership.
const AppContractDuration = RegisterDuration

// requestSigner signs a request as the node; crypto.Module satisfies it.
type requestSigner interface {
	Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error)
}

// contractSigner signs a contract as its issuer and, when it holds the key,
// its subject; auth.Module satisfies it.
type contractSigner interface {
	SignContract(ctx *astral.Context, contract *auth.SignedContract) error
}

// contractRequests files the ContractRequests of the apps this node hosts and
// carries out their Approvals (the host worker). It reaches SwarmState only
// through nodes.SwarmState and reads what entries mean only through
// user.SwarmView.
//
// Nothing constructs it in the running node yet: G7 builds it in
// LoadDependencies, registers apphost.request_contract and starts Run.
type contractRequests struct {
	node     *astral.Identity
	state    nodesmod.SwarmState
	view     usermod.SwarmView
	requests requestSigner
	signer   contractSigner
	now      func() time.Time
	limiter  *requestLimiter

	// mu makes finding a live request and filing a new one one step, so two
	// calls of one app never file two requests.
	mu sync.Mutex

	// filed holds the requests this process filed, by entry ID, so a request
	// SwarmState holds (ErrSwarmEntryHeld) is still found and waited on.
	filed sig.Map[string, *user.SignedContractRequest]
	// signed holds the contracts this process signed, by the entry ID of the
	// request they carry out, so an Approval is carried out once even before
	// SwarmState stores the contract.
	signed sig.Map[string, *auth.SignedContract]
	// changed is closed and replaced after every poll; Wait sleeps on it.
	changed atomic.Pointer[chan struct{}]
}

func newContractRequests(node *astral.Identity, state nodesmod.SwarmState, view usermod.SwarmView,
	requests requestSigner, signer contractSigner) *contractRequests {
	cr := &contractRequests{
		node:     node,
		state:    state,
		view:     view,
		requests: requests,
		signer:   signer,
		now:      time.Now,
		limiter:  newRequestLimiter(ContractRequestWindow, ContractRequestsPerApp, ContractRequestsPerNode),
	}
	changed := make(chan struct{})
	cr.changed.Store(&changed)
	return cr
}

// File returns the entry ID of the ContractRequest that asks permits for app.
// A live request this node filed for app with the same permits (pending and
// unexpired, or approved) is returned as is; otherwise File signs a new
// request, with the default one-hour Timeout, and adds it to SwarmState. A
// new request counts against the rate limits; a refused one is
// ErrContractRequestRateLimited.
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

	now := cr.now()
	if !cr.limiter.allow(app, now) {
		return nil, apphostmod.ErrContractRequestRateLimited
	}

	filedAt := astral.Time(now)
	req := &user.ContractRequest{
		FiledBy:  cr.node,
		Identity: app,
		Permits:  permits,
		FiledAt:  filedAt,
		Timeout:  user.RequestDeadline(filedAt),
	}
	signature, err := cr.requests.Sign(ctx, secp256k1.FromIdentity(cr.node), req)
	if err != nil {
		return nil, fmt.Errorf("sign contract request: %w", err)
	}
	signed := &user.SignedContractRequest{ContractRequest: req, FiledBySig: signature}
	id, err := astral.ResolveObjectID(signed)
	if err != nil {
		return nil, err
	}

	// why a held request counts as filed: SwarmState admits it once this
	// node's membership arrives, and filing again would add a second one.
	if err = cr.state.AddSwarmEntry(ctx, signed); err != nil && !errors.Is(err, nodesmod.ErrSwarmEntryHeld) {
		return nil, fmt.Errorf("add contract request: %w", err)
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
		case user.RequestApproved:
			return c.id
		case user.RequestPending:
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

// ownRequest returns the ContractRequest with entry ID id if this node filed
// it, or nil.
func (cr *contractRequests) ownRequest(id *astral.ObjectID) *user.SignedContractRequest {
	if req, ok := cr.filed.Get(id.String()); ok {
		return req
	}

	obj, err := cr.state.SwarmEntry(id)
	if err != nil {
		return nil
	}
	req, ok := obj.(*user.SignedContractRequest)
	if !ok || req.ContractRequest == nil || !req.FiledBy.IsEqual(cr.node) || req.Identity.IsZero() {
		return nil
	}
	return req
}

// Poll carries out, once each, the Approvals of ContractRequests this node
// filed: for each approved request not carried out yet, it signs the app's
// contract and adds it to SwarmState. It then wakes every Wait.
//
// The contract names exactly the identity and permits of the approved request
// and carries the entry ID of the Approval, so one app's Approval never
// becomes another app's contract. A request another node filed is that
// node's to carry out, even when it names an app hosted here: an app moved
// here files its own request.
func (cr *contractRequests) Poll(ctx *astral.Context) error {
	defer cr.notify()

	var errs []error
	for _, a := range cr.approvalsByRequest() {
		req := cr.ownRequest(a.request)
		if req == nil {
			continue
		}
		if cr.view.RequestStatus(a.request) != user.RequestApproved {
			continue
		}
		if cr.carriedOut(a.request, req) != nil {
			continue
		}
		if err := cr.carryOut(ctx, a.request, a.approval, req); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type requestApproval struct {
	request  *astral.ObjectID
	approval *astral.ObjectID
}

// approvalsByRequest returns one Approval per request among those that touch
// this node: the one with the lowest entry ID, so every poll picks the same
// one when several nodes approved a request.
func (cr *contractRequests) approvalsByRequest() []requestApproval {
	byRequest := map[string]requestApproval{}
	for _, a := range cr.view.ApprovalsTouching(cr.node) {
		if a == nil || a.Approval == nil || a.RequestID.IsZero() {
			continue
		}
		aid, err := astral.ResolveObjectID(a)
		if err != nil {
			continue
		}
		key := a.RequestID.String()
		if prev, ok := byRequest[key]; ok && prev.approval.String() <= aid.String() {
			continue
		}
		byRequest[key] = requestApproval{request: a.RequestID, approval: aid}
	}

	list := make([]requestApproval, 0, len(byRequest))
	for _, a := range byRequest {
		list = append(list, a)
	}
	return list
}

// carryOut signs the contract of an approved request and adds it to
// SwarmState.
func (cr *contractRequests) carryOut(ctx *astral.Context, reqID, approvalID *astral.ObjectID, req *user.SignedContractRequest) error {
	signed := &auth.SignedContract{Contract: &auth.Contract{
		Issuer:     cr.node,
		Subject:    req.Identity,
		Permits:    req.Permits,
		ExpiresAt:  astral.Time(cr.contractExpiry()),
		ApprovalID: approvalID,
	}}
	if err := cr.signer.SignContract(ctx, signed); err != nil {
		return fmt.Errorf("sign contract for %v: %w", req.Identity, err)
	}
	if err := cr.state.AddSwarmEntry(ctx, signed); err != nil && !errors.Is(err, nodesmod.ErrSwarmEntryHeld) {
		return fmt.Errorf("add contract for %v: %w", req.Identity, err)
	}

	cr.signed.Replace(reqID.String(), signed)
	return nil
}

// contractExpiry returns when an app contract signed now expires: after
// AppContractDuration, or when this node's membership expires, if earlier.
//
// why: a link never outlives its parent.
func (cr *contractRequests) contractExpiry() time.Time {
	expiresAt := cr.now().Add(AppContractDuration)
	if m, ok := cr.view.Membership(cr.node); ok && m.SignedContract != nil && m.Contract != nil {
		if t := m.ExpiresAt.Time(); t.Before(expiresAt) {
			expiresAt = t
		}
	}
	return expiresAt
}

// carriedOut returns the contract this node signed for the request with entry
// ID reqID, carrying out one of its Approvals, or nil.
func (cr *contractRequests) carriedOut(reqID *astral.ObjectID, req *user.SignedContractRequest) *auth.SignedContract {
	if c, ok := cr.signed.Get(reqID.String()); ok {
		return c
	}

	for _, a := range cr.view.ApprovalsTouching(cr.node) {
		if a == nil || a.Approval == nil || !a.RequestID.IsEqual(reqID) {
			continue
		}
		aid, err := astral.ResolveObjectID(a)
		if err != nil {
			continue
		}
		for _, c := range cr.view.CarriedOut(aid) {
			if c != nil && c.Contract != nil && c.Issuer.IsEqual(cr.node) &&
				c.Subject.IsEqual(req.Identity) && req.Covers(c.Contract) {
				return c
			}
		}
	}
	return nil
}

// Outcome returns the result of the request with entry ID reqID this node
// filed: the contract carrying it out, ErrContractDeclined, or
// ErrContractRequestExpired. Both are nil while the request is pending, or
// approved but not carried out yet.
//
// why a Decline wins over a signed contract: it is the swarm's verdict, and
// the chain check fails from then on (decided 2026-11-25).
func (cr *contractRequests) Outcome(reqID *astral.ObjectID) (*auth.SignedContract, error) {
	req := cr.ownRequest(reqID)
	if req == nil {
		return nil, fmt.Errorf("no contract request %v filed by this node", reqID)
	}
	switch cr.view.RequestStatus(reqID) {
	case user.RequestDeclined:
		return nil, apphostmod.ErrContractDeclined
	case user.RequestApproved:
		return cr.carriedOut(reqID, req), nil
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
// for Approvals when the head moved, and wakes every Wait on each tick, so a
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
