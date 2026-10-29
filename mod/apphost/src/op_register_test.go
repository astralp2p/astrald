package apphost

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// The steps apphost.register takes that decide whether a failed registration
// leaves an orphan key: the relay contract entering the auth index, and the key
// entering the store or the crypto index.
const (
	stepIndexRelay = "index relay contract"
	stepStoreKey   = "store key"
	stepIndexKey   = "index key"
)

// registerSteps records, in order, the steps apphost.register takes and the
// authorizer assignments it records. It refuses to index a relay contract when
// indexErr is set, and refuses an assignment when setErr is set.
type registerSteps struct {
	mu       sync.Mutex
	steps    []string
	assigned []*auth.AuthorizerAssignment
	indexErr error
	setErr   error
}

func (r *registerSteps) assign(a *auth.AuthorizerAssignment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assigned = append(r.assigned, a)
}

func (r *registerSteps) assignments() []*auth.AuthorizerAssignment {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.assigned)
}

func (r *registerSteps) record(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *registerSteps) taken() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.steps)
}

// registerAuth signs every contract without checking it and records the relay
// contract it indexes.
type registerAuth struct {
	serveAppsSigner
	steps *registerSteps
}

func (a registerAuth) IndexContract(_ *astral.Context, sc *auth.SignedContract) error {
	if len(sc.HasPermit(nodes.RelayForAction{}.ObjectType())) == 0 {
		return nil
	}
	if a.steps.indexErr != nil {
		return a.steps.indexErr
	}
	a.steps.record(stepIndexRelay)
	return nil
}

func (a registerAuth) SetAuthorizer(_ *astral.Context, assignment *auth.AuthorizerAssignment) error {
	if a.steps.setErr != nil {
		return a.steps.setErr
	}
	a.steps.assign(assignment)
	return nil
}

// registerKeys records every key handed to the crypto index.
type registerKeys struct {
	cryptomod.Module
	steps *registerSteps
}

func (k registerKeys) AddToIndex(obj astral.Object) error {
	if _, ok := obj.(*crypto.PrivateKey); ok {
		k.steps.record(stepIndexKey)
	}
	return nil
}

// registerStore records every key it stores and keeps nothing.
type registerStore struct {
	serveAppsStore
	steps *registerSteps
}

func (s registerStore) Store(_ *astral.Context, _ objectsmod.Repository, obj astral.Object) (*astral.ObjectID, error) {
	if _, ok := obj.(*crypto.PrivateKey); ok {
		s.steps.record(stepStoreKey)
	}
	return &astral.ObjectID{}, nil
}

// registerWith answers a module that completes apphost.register and records
// its steps in steps.
func registerWith(t *testing.T, steps *registerSteps) *Module {
	t.Helper()

	mod := serveAppsRegistrar(t)
	mod.Auth = registerAuth{steps: steps}
	mod.Crypto = registerKeys{steps: steps}
	mod.Objects = registerStore{steps: steps}

	w := newRecordingWriter()
	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	return mod
}

// TestRegisterIndexesTheRelayContractBeforeTheKey: the key apphost.register
// mints enters the store and the crypto index only once its relay contract to
// the node is indexed.
func TestRegisterIndexesTheRelayContractBeforeTheKey(t *testing.T) {
	steps := &registerSteps{}
	registerWith(t, steps)

	want := []string{stepIndexRelay, stepStoreKey, stepIndexKey}
	if got := steps.taken(); !slices.Equal(got, want) {
		t.Fatalf("apphost.register took steps %q; want %q", got, want)
	}
}

// TestRegisterLeavesNoKeyWhenTheRelayContractFails injects a failure between
// minting the key and indexing its relay contract: the registration stores and
// indexes no key, and issues no token.
func TestRegisterLeavesNoKeyWhenTheRelayContractFails(t *testing.T) {
	steps := &registerSteps{indexErr: errors.New("index refused")}
	mod := registerWith(t, steps)

	if got := steps.taken(); len(got) != 0 {
		t.Fatalf("a failed relay contract left steps %q; want no key stored or indexed", got)
	}

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("a failed registration issued %d tokens; want none", len(tokens))
	}
}

// delegatedRegistrar is a registrar whose registration policy is delegated to
// node, which answers one question.
func delegatedRegistrar(t *testing.T, steps *registerSteps, node *delegateNode) *Module {
	t.Helper()

	mod := serveAppsRegistrar(t)
	mod.node = node
	mod.Auth = registerAuth{steps: steps}
	mod.Crypto = registerKeys{steps: steps}
	mod.Objects = registerStore{steps: steps}

	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := mod.policy.AppRegister.Set(astral.NewContext(nil), d); err != nil {
		t.Fatalf("set delegate: %v", err)
	}
	return mod
}

func issuedTokens(t *testing.T, mod *Module) []dbAccessToken {
	t.Helper()

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	return tokens
}

// TestRegisterRefusedProvisionsNothing: the query is accepted before the
// policy decides, so a refusal answers an error object; it mints no key and
// issues no token.
func TestRegisterRefusedProvisionsNothing(t *testing.T) {
	steps := &registerSteps{}
	mod := delegatedRegistrar(t, steps, refusingNode())
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("a delegated registration was rejected before the decision: %v", err)
	}
	awaitServeAppsClose(t, w)

	if w.written() == 0 {
		t.Fatal("a refused registration answered nothing")
	}
	if got := steps.taken(); len(got) != 0 {
		t.Fatalf("a refused registration took steps %q", got)
	}
	if tokens := issuedTokens(t, mod); len(tokens) != 0 {
		t.Fatalf("a refused registration issued %d tokens", len(tokens))
	}
}

// TestRegisterRecordsTheDecisionsAuthorizers: the assignments a delegate names
// are recorded with the new identity as actor, after the key is in place and
// before the token is issued; the delegate's own copy stays actor-less.
func TestRegisterRecordsTheDecisionsAuthorizers(t *testing.T) {
	steps := &registerSteps{}
	named := &auth.AuthorizerAssignment{Action: "mod.auth.see_objects_action", Endpoint: "astral://control-plane:control.authorize"}
	mod := delegatedRegistrar(t, steps, newDelegateNode(&apphost.AppRegisterDecision{
		Allow:       true,
		Authorizers: []*auth.AuthorizerAssignment{named},
	}))
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	tokens := issuedTokens(t, mod)
	if len(tokens) != 1 {
		t.Fatalf("issued %d tokens, want one", len(tokens))
	}
	assigned := steps.assignments()
	if len(assigned) != 1 {
		t.Fatalf("recorded %d assignments, want one", len(assigned))
	}
	if assigned[0].Action != named.Action || assigned[0].Endpoint != named.Endpoint {
		t.Fatalf("recorded %+v, want the delegate's assignment", assigned[0])
	}
	if assigned[0].Actor == nil || !assigned[0].Actor.IsEqual(tokens[0].Identity) {
		t.Fatalf("recorded actor %v, want the new identity %v", assigned[0].Actor, tokens[0].Identity)
	}
	if named.Actor != nil {
		t.Fatal("the delegate's assignment was changed in place")
	}
	if want := []string{stepIndexRelay, stepStoreKey, stepIndexKey}; !slices.Equal(steps.taken(), want) {
		t.Fatalf("apphost.register took steps %q; want %q", steps.taken(), want)
	}
}

// TestRegisterIssuesNoTokenWhenAnAuthorizerFails: an assignment the auth
// module cannot record fails the registration after the identity exists, and
// no token is issued.
func TestRegisterIssuesNoTokenWhenAnAuthorizerFails(t *testing.T) {
	steps := &registerSteps{setErr: errors.New("endpoint names no transport")}
	mod := delegatedRegistrar(t, steps, newDelegateNode(&apphost.AppRegisterDecision{
		Allow:       true,
		Authorizers: []*auth.AuthorizerAssignment{{Action: "mod.auth.see_objects_action", Endpoint: "ftp://nowhere"}},
	}))
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	if tokens := issuedTokens(t, mod); len(tokens) != 0 {
		t.Fatalf("a failed assignment issued %d tokens", len(tokens))
	}
	if w.written() == 0 {
		t.Fatal("a failed assignment answered nothing")
	}
}

// TestRegisterRefusesADecisionNamingAnActor: the node names the actor, so a
// delegate that names one has sent a decision the node cannot apply.
func TestRegisterRefusesADecisionNamingAnActor(t *testing.T) {
	steps := &registerSteps{}
	mod := delegatedRegistrar(t, steps, newDelegateNode(&apphost.AppRegisterDecision{
		Allow: true,
		Authorizers: []*auth.AuthorizerAssignment{{
			Action: "mod.auth.see_objects_action", Endpoint: "astral://control-plane:control.authorize", Actor: astral.GenerateIdentity(),
		}},
	}))
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register rejected before the decision: %v", err)
	}
	awaitServeAppsClose(t, w)

	if got := steps.taken(); len(got) != 0 {
		t.Fatalf("a refused registration took steps %q", got)
	}
	if tokens := issuedTokens(t, mod); len(tokens) != 0 {
		t.Fatalf("a refused registration issued %d tokens", len(tokens))
	}
}
