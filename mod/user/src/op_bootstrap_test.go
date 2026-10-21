package user

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/bip137sig"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	bip137src "github.com/astralp2p/astrald/mod/bip137sig/src"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// bootstrapWords are the words of tests/e2e/import-user-software-key.
const bootstrapWords = "horse soldier imitate stool square buyer verb party enjoy " +
	"result jazz rabbit trigger file benefit cloth term change"

// bootstrapUser is the identity bootstrapWords derive along m/1791'/0'/0'/0/0.
//
// note: computed outside astrald by a pure-Python BIP-39 and BIP-32
// derivation, which reproduces the BIP-39 "TREZOR" vector, BIP-32 test
// vector 1, and the m/44' identity tests/e2e/import-user-software-key pins.
const bootstrapUser = "02564ba75b0fc5869e12c0c89e56a9c4fbec9d23ed01e6d240aac80532fcb10af1"

// claimAuth is the auth module user.bootstrap reaches: the claim rule answers
// claimable, the node signs as subject with nodeKey, and a contract verifies
// by its two signatures.
type claimAuth struct {
	authmod.Module
	nodeKey   *crypto.PrivateKey
	claimable bool
}

func (a *claimAuth) IsNodeClaim(*astral.Context, *astral.Identity, *auth.Contract) bool {
	return a.claimable
}

func (a *claimAuth) SignSubject(_ *astral.Context, sc *auth.SignedContract) (*crypto.Signature, error) {
	sig, err := secp256k1sig.SignASN1(a.nodeKey, sc.SignableHash())
	sc.SubjectSig = sig
	return sig, err
}

func (a *claimAuth) VerifyContract(sc *auth.SignedContract) error {
	err := secp256k1sig.VerifyASN1(secp256k1.FromIdentity(sc.Issuer), sc.SignableHash(), sc.IssuerSig)
	if err != nil {
		return err
	}
	return secp256k1sig.VerifyASN1(secp256k1.FromIdentity(sc.Subject), sc.SignableHash(), sc.SubjectSig)
}

var errStoreFailed = errors.New("store failed")

// memObjects keeps every stored object and fails the store of objects of type
// failType.
type memObjects struct {
	objectsmod.Module

	mu       sync.Mutex
	failType string
	stored   []astral.Object
}

func (o *memObjects) WriteDefault() objectsmod.Repository { return nil }

func (o *memObjects) Store(_ *astral.Context, _ objectsmod.Repository, obj astral.Object) (*astral.ObjectID, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if obj.ObjectType() == o.failType {
		return nil, errStoreFailed
	}
	o.stored = append(o.stored, obj)
	return astral.ResolveObjectID(obj)
}

func (o *memObjects) objects() []astral.Object {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]astral.Object(nil), o.stored...)
}

// keyIndex keeps every object handed to AddToIndex.
type keyIndex struct {
	cryptomod.Module

	mu      sync.Mutex
	indexed []astral.Object
}

func (c *keyIndex) AddToIndex(obj astral.Object) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.indexed = append(c.indexed, obj)
	return nil
}

func (c *keyIndex) objects() []astral.Object {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]astral.Object(nil), c.indexed...)
}

type bootstrapFixture struct {
	mod     *Module
	nodeID  *astral.Identity
	auth    *claimAuth
	objects *memObjects
	keys    *keyIndex
}

// newBootstrapFixture is an unclaimed node with the real bip137sig derivation.
func newBootstrapFixture() *bootstrapFixture {
	nodeKey := secp256k1.New()
	f := &bootstrapFixture{
		nodeID:  secp256k1.Identity(secp256k1.PublicKey(nodeKey)),
		auth:    &claimAuth{nodeKey: nodeKey, claimable: true},
		objects: &memObjects{},
		keys:    &keyIndex{},
	}
	f.mod = &Module{
		Deps:         Deps{Auth: f.auth, Objects: f.objects, Crypto: f.keys},
		OptionalDeps: OptionalDeps{Bip137sig: &bip137src.Module{}},
		node:         &identityNode{id: f.nodeID},
	}
	return f
}

// identityOf derives the user identity of words along UserKeyPath.
func identityOf(t *testing.T, words string) *astral.Identity {
	t.Helper()
	seed, err := bip137sig.MnemonicToSeed(strings.Fields(words), "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	key, err := (&bip137src.Module{}).DeriveKey(seed, "m/1791'/0'/0'/0/0")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return secp256k1.Identity(secp256k1.PublicKey(&key))
}

// routeBootstrap sends one user.bootstrap query with origin, writes input to
// the op when it is set, and answers what the op wrote once it returned. A
// rejected query answers the rejection.
func routeBootstrap(t *testing.T, f *bootstrapFixture, caller *astral.Identity, origin, queryString string, input astral.Object) ([]astral.Object, error) {
	t.Helper()

	op, err := routing.NewOp(f.mod.OpBootstrap)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(caller, f.nodeID, queryString, nil))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	w := &answerWriter{}
	requester, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		return nil, err
	}
	if input != nil {
		if err := channel.NewSender(requester).Send(input); err != nil {
			t.Fatalf("send input: %v", err)
		}
	}
	requester.Close()

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}
	return w.objects(t), nil
}

// onlyResult fails unless the op answered exactly one BootstrapResult.
func onlyResult(t *testing.T, objs []astral.Object) *user.BootstrapResult {
	t.Helper()
	if len(objs) != 1 {
		t.Fatalf("the op answered %d objects (%v); want one result", len(objs), objs)
	}
	result, ok := objs[0].(*user.BootstrapResult)
	if !ok {
		t.Fatalf("the op answered %v; want a mod.user.bootstrap_result", objs[0])
	}
	return result
}

// checkClaimed fails unless the active contract names userID as issuer and the
// node as subject, and the node stored and indexed the user's key.
func checkClaimed(t *testing.T, f *bootstrapFixture, userID *astral.Identity) {
	t.Helper()

	ac := f.mod.ActiveContract()
	if ac == nil {
		t.Fatal("the node is unclaimed")
	}
	if !ac.Issuer.IsEqual(userID) || !ac.Subject.IsEqual(f.nodeID) {
		t.Fatalf("active contract %v -> %v; want %v -> %v", ac.Issuer, ac.Subject, userID, f.nodeID)
	}
	if err := f.auth.VerifyContract(ac); err != nil {
		t.Fatalf("active contract signatures: %v", err)
	}
	if !holdsKeyOf(f.keys.objects(), userID) {
		t.Fatal("the user's private key is not indexed")
	}
	if !holdsKeyOf(f.objects.objects(), userID) {
		t.Fatal("the user's private key is not stored")
	}
}

func holdsKeyOf(objs []astral.Object, userID *astral.Identity) bool {
	for _, obj := range objs {
		key, ok := obj.(*crypto.PrivateKey)
		if ok && secp256k1.Identity(secp256k1.PublicKey(key)).IsEqual(userID) {
			return true
		}
	}
	return false
}

// checkUnchanged fails unless the node is unclaimed and stored nothing.
func checkUnchanged(t *testing.T, f *bootstrapFixture) {
	t.Helper()
	if f.mod.ActiveContract() != nil {
		t.Fatal("the node is claimed")
	}
	if n := len(f.objects.objects()); n != 0 {
		t.Fatalf("the node stored %d objects; want none", n)
	}
	if n := len(f.keys.objects()); n != 0 {
		t.Fatalf("the node indexed %d keys; want none", n)
	}
}

// checkNoWords fails if any stored object is a string16, the type the words
// travel as.
func checkNoWords(t *testing.T, f *bootstrapFixture) {
	t.Helper()
	for _, obj := range f.objects.objects() {
		if _, ok := obj.(*astral.String16); ok {
			t.Fatalf("the node stored a string16: %v", obj)
		}
	}
}

// TestBootstrapClaimsWithGivenWords: the words claim the node for the user
// they derive along m/1791'/0'/0'/0/0, and the reply carries no words.
func TestBootstrapClaimsWithGivenWords(t *testing.T) {
	f := newBootstrapFixture()

	objs, err := routeBootstrap(t, f, f.nodeID, "", "user.bootstrap", astral.NewString16(bootstrapWords))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	result := onlyResult(t, objs)

	if result.Identity.String() != bootstrapUser {
		t.Fatalf("identity %v; want %v", result.Identity, bootstrapUser)
	}
	if result.Mnemonic != "" {
		t.Fatal("the reply carries the words the caller sent")
	}
	checkClaimed(t, f, result.Identity)
	checkNoWords(t, f)
}

// TestBootstrapGeneratesWords: -generate claims the node and returns 24 words
// that derive the user it claimed for.
func TestBootstrapGeneratesWords(t *testing.T) {
	f := newBootstrapFixture()

	objs, err := routeBootstrap(t, f, f.nodeID, astral.OriginLocal, "user.bootstrap?generate=true", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	result := onlyResult(t, objs)

	words := strings.Fields(string(result.Mnemonic))
	if len(words) != 24 {
		t.Fatalf("the reply carries %d words; want 24", len(words))
	}
	if !identityOf(t, string(result.Mnemonic)).IsEqual(result.Identity) {
		t.Fatal("the returned words do not derive the returned identity")
	}
	checkClaimed(t, f, result.Identity)
	checkNoWords(t, f)
}

// TestBootstrapRejects: every refused query is rejected before it is accepted,
// and leaves the node as it was.
func TestBootstrapRejects(t *testing.T) {
	cases := []struct {
		name    string
		origin  string
		caller  func(f *bootstrapFixture) *astral.Identity
		claimed bool
		code    uint8
	}{
		{"network origin", astral.OriginNetwork, func(f *bootstrapFixture) *astral.Identity { return f.nodeID }, false, astral.CodeRejected},
		{"mcp origin", astral.OriginMCP, func(f *bootstrapFixture) *astral.Identity { return f.nodeID }, false, astral.CodeRejected},
		{"claimed node", "", func(f *bootstrapFixture) *astral.Identity { return f.nodeID }, true, 2},
		{"caller other than the node", "", func(*bootstrapFixture) *astral.Identity { return astral.GenerateIdentity() }, false, 4},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBootstrapFixture()
			owner := astral.GenerateIdentity()
			if c.claimed {
				err := f.mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: owner}})
				if err != nil {
					t.Fatalf("seed active contract: %v", err)
				}
			}

			_, err := routeBootstrap(t, f, c.caller(f), c.origin, "user.bootstrap?generate=true", nil)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) || rejected.Code != c.code {
				t.Fatalf("got %v; want a rejection with code %d", err, c.code)
			}
			if n := len(f.objects.objects()); n != 0 {
				t.Fatalf("the node stored %d objects; want none", n)
			}
			if c.claimed && !f.mod.ActiveContract().Issuer.IsEqual(owner) {
				t.Fatal("the active contract changed")
			}
			if !c.claimed && f.mod.ActiveContract() != nil {
				t.Fatal("the node is claimed")
			}
		})
	}
}

// TestBootstrapRefusesWithoutChange: a refusal after the query is accepted
// answers its error and leaves the node unclaimed with nothing stored.
func TestBootstrapRefusesWithoutChange(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(f *bootstrapFixture)
		words   string
		want    string
	}{
		{"invalid checksum", func(*bootstrapFixture) {}, strings.Repeat("horse ", 18), bip137sig.ErrInvalidMnemonic.Error()},
		{"wrong word count", func(*bootstrapFixture) {}, "horse soldier", bip137sig.ErrInvalidMnemonicWordCount.Error()},
		{"claim rule refuses the user", func(f *bootstrapFixture) { f.auth.claimable = false }, bootstrapWords, errClaimRefused.Error()},
		{"no bip137sig module", func(f *bootstrapFixture) { f.mod.Bip137sig = nil }, bootstrapWords, errNoKeyDeriver.Error()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBootstrapFixture()
			c.prepare(f)

			objs, err := routeBootstrap(t, f, f.nodeID, "", "user.bootstrap", astral.NewString16(c.words))
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
				t.Fatalf("the op answered %v; want one error_message", objs)
			}
			if got := objs[0].(astral.Error).Error(); got != c.want {
				t.Fatalf("error %q; want %q", got, c.want)
			}
			checkUnchanged(t, f)
		})
	}
}

// TestBootstrapRetryCompletesClaim: a claim that fails after the user's key
// is stored leaves the node unclaimed, and the same words complete it.
func TestBootstrapRetryCompletesClaim(t *testing.T) {
	f := newBootstrapFixture()
	f.objects.failType = auth.SignedContract{}.ObjectType()

	words := strings.Fields(bootstrapWords)
	_, err := f.mod.bootstrap(nil, f.nodeID, words, "")
	if !errors.Is(err, errStoreFailed) {
		t.Fatalf("first call: %v; want the injected store failure", err)
	}
	if f.mod.ActiveContract() != nil {
		t.Fatal("the node is claimed after a failed contract store")
	}

	f.objects.failType = ""
	userID, err := f.mod.bootstrap(nil, f.nodeID, words, "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	checkClaimed(t, f, userID)
}

// TestClaimRefusesSecondClaim: a claim on a claimed node answers errClaimed
// and leaves the active contract as it was.
func TestClaimRefusesSecondClaim(t *testing.T) {
	f := newBootstrapFixture()
	owner := astral.GenerateIdentity()
	err := f.mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: owner}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	err = f.mod.claim(nil, nodeContract(astral.GenerateIdentity(), f.nodeID))
	if !errors.Is(err, errClaimed) {
		t.Fatalf("claim: %v; want errClaimed", err)
	}
	if !f.mod.ActiveContract().Issuer.IsEqual(owner) {
		t.Fatal("the active contract changed")
	}
	if n := len(f.objects.objects()); n != 0 {
		t.Fatalf("the node stored %d objects; want none", n)
	}
}
