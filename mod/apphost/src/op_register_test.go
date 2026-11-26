package apphost

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
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
	stepSignPermit = "sign permit contract"
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

// registerAuth signs every contract without checking it, and records the relay
// contract it indexes and every permit contract it signs.
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

func (a registerAuth) SignContract(*astral.Context, *auth.SignedContract) error {
	a.steps.record(stepSignPermit)
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
	if err := register(t, mod, &apphost.RegisterRequest{}, w); err != nil {
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

func issuedTokens(t *testing.T, mod *Module) []dbAccessToken {
	t.Helper()

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	return tokens
}

// sudoPermit is an unconstrained mod.auth.sudo_action permit.
func sudoPermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.SudoAction{}.ObjectType())}
}

// TestRegisterRefusesASudoContractPermit: no app is approved for
// mod.auth.sudo_action, so a registration asking it is refused before any
// key is minted.
func TestRegisterRefusesASudoContractPermit(t *testing.T) {
	steps := &registerSteps{}
	mod := serveAppsRegistrar(t)
	mod.Auth = registerAuth{steps: steps}
	mod.Crypto = registerKeys{steps: steps}
	mod.Objects = registerStore{steps: steps}
	w := newRecordingWriter()

	req := &apphost.RegisterRequest{ContractPermits: []*auth.Permit{sudoPermit()}}
	if err := register(t, mod, req, w); err != nil {
		t.Fatalf("apphost.register rejected instead of answering: %v", err)
	}

	if got := w.refusal(t); !strings.Contains(got, "mod.auth.sudo_action") {
		t.Fatalf("answered %q; want the refusal naming the permit", got)
	}
	if got := steps.taken(); len(got) != 0 {
		t.Fatalf("a refused registration took steps %q", got)
	}
	if tokens := issuedTokens(t, mod); len(tokens) != 0 {
		t.Fatalf("a refused registration issued %d tokens", len(tokens))
	}
}

// TestProvisionAppSignsASudoContractPermit: sys.register_manager provisions the
// manager through ProvisionApp, which files no request, so a
// mod.auth.sudo_action contract permit is signed into its node->app contract.
func TestProvisionAppSignsASudoContractPermit(t *testing.T) {
	steps := &registerSteps{}
	mod := serveAppsRegistrar(t)
	mod.Auth = registerAuth{steps: steps}
	mod.Crypto = registerKeys{steps: steps}
	mod.Objects = registerStore{steps: steps}

	token, err := mod.ProvisionApp(astral.NewContext(nil), []*auth.Permit{sudoPermit()})
	if err != nil {
		t.Fatalf("ProvisionApp: %v", err)
	}
	if token == nil || token.Identity.IsZero() {
		t.Fatalf("ProvisionApp answered %v; want an access token", token)
	}
	want := []string{stepIndexRelay, stepStoreKey, stepIndexKey, stepSignPermit}
	if got := steps.taken(); !slices.Equal(got, want) {
		t.Fatalf("ProvisionApp took steps %q; want %q", got, want)
	}
}
