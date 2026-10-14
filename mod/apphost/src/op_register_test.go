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
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// The steps apphost.register takes that decide whether a minted key is
// protected: the relay contract entering the auth index, and the key entering
// the store or the crypto index.
const (
	stepIndexRelay = "index relay contract"
	stepStoreKey   = "store key"
	stepIndexKey   = "index key"
)

// registerSteps records, in order, the steps apphost.register takes, and
// refuses to index a relay contract when indexErr is set.
type registerSteps struct {
	mu       sync.Mutex
	steps    []string
	indexErr error
	assigned []*auth.AuthorizerAssignment
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

// SetAuthorizer records the assignment registration makes.
func (a registerAuth) SetAuthorizer(_ *astral.Context, assignment *auth.AuthorizerAssignment) error {
	a.steps.mu.Lock()
	defer a.steps.mu.Unlock()
	a.steps.assigned = append(a.steps.assigned, assignment)
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
	return registerUnder(t, steps, nil)
}

// registerUnder is registerWith under policy; nil keeps accept-all.
func registerUnder(t *testing.T, steps *registerSteps, policy apphostmod.AppRegisterPolicy) *Module {
	t.Helper()

	mod := serveAppsRegistrar(t)
	mod.appRegisterPolicy = policy
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

// TestRegisterAssignsTheDecisionsAuthorizersToTheNewApp: an authorizer the
// decision names is assigned with the minted identity as its actor.
func TestRegisterAssignsTheDecisionsAuthorizersToTheNewApp(t *testing.T) {
	steps := &registerSteps{}
	mod := registerUnder(t, steps, func(_ *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
		return &apphost.AppRegisterDecision{
			Allow:        true,
			GrantPermits: req.GrantPermits,
			Authorizers: []*auth.AuthorizerAssignment{
				{Action: "mod.auth.see_objects_action", Endpoint: "astral://media:auth.evaluate"},
			},
		}, nil
	})

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil || len(tokens) != 1 {
		t.Fatalf("tokens: %v, %v", len(tokens), err)
	}

	if len(steps.assigned) != 1 {
		t.Fatalf("assigned %v authorizers, want 1", len(steps.assigned))
	}
	if a := steps.assigned[0]; !a.Actor.IsEqual(tokens[0].Identity) || a.Endpoint != "astral://media:auth.evaluate" {
		t.Fatalf("assigned %+v, want the new app %v as actor", a, tokens[0].Identity)
	}
}

// TestRegisterRefusedProvisionsNothing: a refusing decision mints no key and
// issues no token, and a policy error refuses the same way.
func TestRegisterRefusedProvisionsNothing(t *testing.T) {
	for name, policy := range map[string]apphostmod.AppRegisterPolicy{
		"refusal": func(*astral.Context, *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
			return &apphost.AppRegisterDecision{Allow: false}, nil
		},
		"error": func(*astral.Context, *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
			return nil, errors.New("delegate unreachable")
		},
	} {
		t.Run(name, func(t *testing.T) {
			steps := &registerSteps{}
			mod := registerUnder(t, steps, policy)

			if got := steps.taken(); len(got) != 0 {
				t.Fatalf("a refused registration took steps %q", got)
			}
			var tokens []dbAccessToken
			if err := mod.db.Find(&tokens).Error; err != nil || len(tokens) != 0 {
				t.Fatalf("a refused registration issued %d tokens (%v)", len(tokens), err)
			}
		})
	}
}
