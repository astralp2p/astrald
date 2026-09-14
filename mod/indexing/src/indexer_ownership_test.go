package indexing

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// policyAuth allows an actor an action type it was granted, and keeps every
// action it was asked about.
//
// why: the embedded nil interface satisfies authmod.Module without implementing
// it. Any method other than Authorize panics.
// note: a ServeObjects grant covers the indexer role alone, so an op naming
// another role is refused.
type policyAuth struct {
	authmod.Module

	mu      sync.Mutex
	grants  map[string]bool
	actions []auth.ActionObject
}

func newPolicyAuth() *policyAuth {
	return &policyAuth{grants: map[string]bool{}}
}

func grantKey(actionType string, actor *astral.Identity) string {
	return actionType + " " + actor.String()
}

func (a *policyAuth) grant(action auth.ActionObject, actor *astral.Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grants[grantKey(action.ObjectType(), actor)] = true
}

func (a *policyAuth) revokeAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grants = map[string]bool{}
}

func (a *policyAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)

	if serve, ok := action.(*auth.ServeObjectsAction); ok && serve.Role != auth.RoleIndexer {
		return false
	}
	return a.grants[grantKey(action.ObjectType(), action.Actor())]
}

func (a *policyAuth) recorded() []auth.ActionObject {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auth.ActionObject(nil), a.actions...)
}

// captureWriter is the caller's end of the connection. It keeps what the op
// writes, and Close marks that the op has returned.
type captureWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed atomic.Bool
	done   chan struct{}
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{done: make(chan struct{})}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *captureWriter) Close() error {
	if w.closed.CompareAndSwap(false, true) {
		close(w.done)
	}
	return nil
}

func (w *captureWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// indexerFixture is a module holding only the indexers tree and policyAuth,
// mounted the way mod/shell mounts it.
type indexerFixture struct {
	t      *testing.T
	ctx    *astral.Context
	root   *memNode
	mod    *Module
	auth   *policyAuth
	scopes *routing.ScopeRouter
}

func newIndexerFixture(t *testing.T) *indexerFixture {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	root := newMemNode(nil, "")
	authority := newPolicyAuth()
	mod := &Module{Deps: Deps{Auth: authority}, indexers: root}

	return &indexerFixture{t: t, ctx: ctx, root: root, mod: mod, auth: authority, scopes: mountLikeShell(t, mod)}
}

// start routes queryString from caller with origin and returns the caller's
// end of the connection once the op has accepted the query.
func (f *indexerFixture) start(caller *astral.Identity, origin string, queryString string) (*captureWriter, error) {
	q := astral.Launch(query.New(caller, caller, queryString, nil))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	w := newCaptureWriter()
	if _, err := f.scopes.RouteQuery(f.ctx, q, w); err != nil {
		return nil, err
	}
	return w, nil
}

// query routes queryString from caller with origin and returns what the op
// wrote once it returned. A rejection returns the router's error and no output.
func (f *indexerFixture) query(caller *astral.Identity, origin string, queryString string) (string, error) {
	f.t.Helper()

	w, err := f.start(caller, origin, queryString)
	if err != nil {
		return "", err
	}

	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		f.t.Fatalf("%s did not return", queryString)
	}
	return w.String(), nil
}

// register stores a registration for owner directly and advances its cursor in
// repository local to version 1.
func (f *indexerFixture) register(owner *astral.Identity, name string) astral.Nonce {
	f.t.Helper()

	nonce, err := f.mod.RegisterIndexer(f.ctx, owner, name)
	if err != nil {
		f.t.Fatalf("register %s: %v", name, err)
	}
	if err := f.mod.UpdateIndexerState(f.ctx, nonce, "local", 1); err != nil {
		f.t.Fatalf("advance cursor: %v", err)
	}
	return nonce
}

// cursor returns the version the registration named by nonce acked in local.
func (f *indexerFixture) cursor(nonce astral.Nonce) uint64 {
	f.t.Helper()

	idxer, err := f.mod.findIndexerByNonce(f.ctx, nonce)
	if err != nil || idxer == nil {
		f.t.Fatalf("find registration %v: %v, %v", nonce, idxer, err)
	}
	version, err := idxer.state(f.ctx, "local")
	if err != nil {
		f.t.Fatalf("read cursor: %v", err)
	}
	return version
}

func (f *indexerFixture) registrations() int {
	subs, _ := f.root.Sub(f.ctx)
	return len(subs)
}

// replyNonce decodes a one-object JSON reply and returns its nonce, or false when
// the reply is not a nonce64.
//
// note: the JSON form of a nonce64 is hex without leading zeros, so a reply is
// compared by value, never against Nonce.String.
func replyNonce(t *testing.T, out string) (astral.Nonce, bool) {
	t.Helper()

	var reply struct {
		Type   string
		Object json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode reply %q: %v", out, err)
	}
	if reply.Type != "nonce64" {
		return 0, false
	}

	var hex string
	if err := json.Unmarshal(reply.Object, &hex); err != nil {
		t.Fatalf("decode nonce %s: %v", reply.Object, err)
	}
	n, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		t.Fatalf("parse nonce %q: %v", hex, err)
	}
	return astral.Nonce(n), true
}

func requireRejected(t *testing.T, name string, err error) {
	t.Helper()

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("%s: got err %v, want a rejection", name, err)
	}
}

// requireOnlyAction checks that the op asked exactly one question, of type want,
// about actor.
func requireOnlyAction(t *testing.T, actions []auth.ActionObject, want auth.ActionObject, actor *astral.Identity) {
	t.Helper()

	if len(actions) != 1 {
		t.Fatalf("made %d authorization calls; want exactly 1", len(actions))
	}
	if actions[0].ObjectType() != want.ObjectType() {
		t.Fatalf("named %q; want %q", actions[0].ObjectType(), want.ObjectType())
	}
	if !actions[0].Actor().IsEqual(actor) {
		t.Fatalf("named actor %v; want the caller %v", actions[0].Actor(), actor)
	}
}

// TestRegisterIndexerRequiresTheIndexerRole: a caller without ServeObjects for
// the indexer role is rejected before any registration exists.
func TestRegisterIndexerRequiresTheIndexerRole(t *testing.T) {
	f := newIndexerFixture(t)
	caller := astral.GenerateIdentity()

	_, err := f.query(caller, "", "indexing.register_indexer?name=search")
	requireRejected(t, "indexing.register_indexer", err)

	actions := f.auth.recorded()
	requireOnlyAction(t, actions, &auth.ServeObjectsAction{}, caller)
	if role := actions[0].(*auth.ServeObjectsAction).Role; role != auth.RoleIndexer {
		t.Fatalf("named role %q; want %q", role, auth.RoleIndexer)
	}
	if n := f.registrations(); n != 0 {
		t.Fatalf("%d registrations exist after a refusal; want none", n)
	}
}

// TestRegisterIndexerKeepsANameToItsOwner is the takeover this guard closes: a
// second identity holding the indexer role asks for a registered name and
// receives an error, never the owner's nonce.
func TestRegisterIndexerKeepsANameToItsOwner(t *testing.T) {
	f := newIndexerFixture(t)
	owner, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.auth.grant(&auth.ServeObjectsAction{}, owner)
	f.auth.grant(&auth.ServeObjectsAction{}, other)

	first, err := f.query(owner, "", "indexing.register_indexer?name=search&out=json")
	if err != nil {
		t.Fatalf("owner register: %v", err)
	}
	idxer, err := f.mod.findIndexerByName(f.ctx, "search")
	if err != nil || idxer == nil {
		t.Fatalf("find registration: %v, %v", idxer, err)
	}
	if got, ok := replyNonce(t, first); !ok || got != idxer.nonce {
		t.Fatalf("owner register answered %q; want nonce %v", first, idxer.nonce)
	}

	taken, err := f.query(other, "", "indexing.register_indexer?name=search&out=json")
	if err != nil {
		t.Fatalf("other register: %v", err)
	}
	if _, ok := replyNonce(t, taken); ok {
		t.Fatalf("another identity received a nonce for a taken name: %q", taken)
	}
	if !strings.Contains(taken, indexing.ErrIndexerNameTaken.Error()) {
		t.Fatalf("another identity got %q; want %q", taken, indexing.ErrIndexerNameTaken)
	}

	again, err := f.query(owner, "", "indexing.register_indexer?name=search&out=json")
	if err != nil {
		t.Fatalf("owner re-register: %v", err)
	}
	if got, ok := replyNonce(t, again); !ok || got != idxer.nonce {
		t.Fatalf("owner re-register answered %q; want nonce %v", again, idxer.nonce)
	}
}

// TestUnregisterIndexerAnswersAnotherIdentityAsMissing: a caller that neither
// owns the registration nor holds AdminObjects is told the nonce is unknown, and
// the registration and its cursor survive.
func TestUnregisterIndexerAnswersAnotherIdentityAsMissing(t *testing.T) {
	f := newIndexerFixture(t)
	owner, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce := f.register(owner, "search")
	f.auth.grant(&auth.ServeObjectsAction{}, other)

	out, err := f.query(other, "", "indexing.unregister_indexer?out=json&nonce="+nonce.String())
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if !strings.Contains(out, indexing.ErrIndexNotFound.Error()) {
		t.Fatalf("another identity got %q; want %q", out, indexing.ErrIndexNotFound)
	}

	requireOnlyAction(t, f.auth.recorded(), &auth.AdminObjectsAction{}, other)
	if version := f.cursor(nonce); version != 1 {
		t.Fatalf("owner's cursor is %d after a refused unregister; want 1", version)
	}
}

// TestUnregisterIndexerLetsAdminObjectsDeleteAnyRegistration: a holder of
// AdminObjects deletes a registration another identity owns.
func TestUnregisterIndexerLetsAdminObjectsDeleteAnyRegistration(t *testing.T) {
	f := newIndexerFixture(t)
	owner, admin := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce := f.register(owner, "search")
	f.auth.grant(&auth.AdminObjectsAction{}, admin)

	out, err := f.query(admin, "", "indexing.unregister_indexer?out=json&nonce="+nonce.String())
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if !strings.Contains(out, `"ack"`) {
		t.Fatalf("admin unregister answered %q; want an ack", out)
	}
	if n := f.registrations(); n != 0 {
		t.Fatalf("%d registrations remain after the admin unregistered; want none", n)
	}
}

// TestUnregisterIndexerOwnerNeedsNoPermit: an owner whose grants are gone still
// deletes its own registration, and the op asks the auth module nothing.
func TestUnregisterIndexerOwnerNeedsNoPermit(t *testing.T) {
	f := newIndexerFixture(t)
	owner := astral.GenerateIdentity()
	nonce := f.register(owner, "search")
	f.auth.revokeAll()

	out, err := f.query(owner, "", "indexing.unregister_indexer?out=json&nonce="+nonce.String())
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if !strings.Contains(out, `"ack"`) {
		t.Fatalf("owner unregister answered %q; want an ack", out)
	}
	if n := len(f.auth.recorded()); n != 0 {
		t.Fatalf("owner unregister made %d authorization calls; want none", n)
	}
	if n := f.registrations(); n != 0 {
		t.Fatalf("%d registrations remain after the owner unregistered; want none", n)
	}
}

// TestSubscribeRequiresTheIndexerRole: an owner without ServeObjects for the
// indexer role is rejected.
func TestSubscribeRequiresTheIndexerRole(t *testing.T) {
	f := newIndexerFixture(t)
	owner := astral.GenerateIdentity()
	nonce := f.register(owner, "search")

	_, err := f.query(owner, "", "indexing.subscribe?nonce="+nonce.String())
	requireRejected(t, "indexing.subscribe", err)
	requireOnlyAction(t, f.auth.recorded(), &auth.ServeObjectsAction{}, owner)
}

// TestSubscribeAnswersAnotherIdentityAsMissing: a caller holding the indexer
// role and AdminObjects still cannot consume a stream it does not own, and the
// owner's cursor does not move.
func TestSubscribeAnswersAnotherIdentityAsMissing(t *testing.T) {
	f := newIndexerFixture(t)
	owner, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce := f.register(owner, "search")
	f.auth.grant(&auth.ServeObjectsAction{}, other)
	f.auth.grant(&auth.AdminObjectsAction{}, other)

	out, err := f.query(other, "", "indexing.subscribe?out=json&nonce="+nonce.String())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !strings.Contains(out, indexing.ErrIndexNotFound.Error()) {
		t.Fatalf("another identity got %q; want %q", out, indexing.ErrIndexNotFound)
	}
	if version := f.cursor(nonce); version != 1 {
		t.Fatalf("owner's cursor is %d after another identity subscribed; want 1", version)
	}
}

// TestSubscribeAcceptsTheOwner: the owner holding the indexer role is accepted
// and, with no change to deliver, waits with its stream open.
//
// note: routing.Op runs the op on a detached context, so the waiting op outlives
// the test and ends with the test binary.
func TestSubscribeAcceptsTheOwner(t *testing.T) {
	f := newIndexerFixture(t)
	owner := astral.GenerateIdentity()
	nonce := f.register(owner, "search")
	f.auth.grant(&auth.ServeObjectsAction{}, owner)

	w, err := f.start(owner, "", "indexing.subscribe?out=json&nonce="+nonce.String())
	if err != nil {
		t.Fatalf("owner subscribe: %v", err)
	}

	select {
	case <-w.done:
		t.Fatalf("owner subscribe returned with %q; want an open stream", w.String())
	case <-time.After(200 * time.Millisecond):
	}
	if out := w.String(); out != "" {
		t.Fatalf("owner subscribe wrote %q with no change to deliver; want nothing", out)
	}
}

// TestIndexerOpsRefuseNetworkOrigin: a query arriving over a link is rejected by
// every op on a registration before the auth module is asked, whatever the
// caller holds.
func TestIndexerOpsRefuseNetworkOrigin(t *testing.T) {
	for _, op := range []string{
		"indexing.register_indexer?name=search",
		"indexing.unregister_indexer?nonce=",
		"indexing.subscribe?nonce=",
	} {
		t.Run(op, func(t *testing.T) {
			f := newIndexerFixture(t)
			owner := astral.GenerateIdentity()
			nonce := f.register(owner, "search")
			f.auth.grant(&auth.ServeObjectsAction{}, owner)
			f.auth.grant(&auth.AdminObjectsAction{}, owner)

			queryString := op
			if strings.HasSuffix(op, "=") {
				queryString += nonce.String()
			}

			_, err := f.query(owner, astral.OriginNetwork, queryString)
			requireRejected(t, op, err)
			if n := len(f.auth.recorded()); n != 0 {
				t.Fatalf("%s made %d authorization calls for a network caller; want none", op, n)
			}
			if version := f.cursor(nonce); version != 1 {
				t.Fatalf("registration changed after a network caller: cursor %d, want 1", version)
			}
		})
	}
}

// TestDeleteUnownedIndexersKeepsOwnedRegistrations: registrations stored before
// owners existed are deleted with their cursors, including one holding a cursor
// for a repository named owner, and an owned registration survives.
func TestDeleteUnownedIndexersKeepsOwnedRegistrations(t *testing.T) {
	f := newIndexerFixture(t)
	owner := astral.GenerateIdentity()
	nonce := f.register(owner, "search")

	for _, legacy := range []struct{ name, repo string }{{"old", "local"}, {"odd", ownerNode}} {
		node, _ := f.root.Create(f.ctx, legacy.name)
		legacyNonce := astral.NewNonce()
		_ = node.Set(f.ctx, &legacyNonce)
		cursor, _ := node.Create(f.ctx, legacy.repo)
		version := astral.Uint64(3)
		_ = cursor.Set(f.ctx, &version)
	}

	deleted, err := f.mod.deleteUnownedIndexers(f.ctx)
	if err != nil {
		t.Fatalf("delete unowned: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d registrations; want 2", deleted)
	}

	subs, _ := f.root.Sub(f.ctx)
	if _, ok := subs["search"]; !ok || len(subs) != 1 {
		t.Fatalf("registrations after cleanup: %v; want only search", subs)
	}
	if version := f.cursor(nonce); version != 1 {
		t.Fatalf("owned cursor is %d after cleanup; want 1", version)
	}
}
