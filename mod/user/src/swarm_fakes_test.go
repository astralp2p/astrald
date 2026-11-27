package user

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// joinClock is the one clock a test's joiner, carrier and swarm read.
type joinClock struct {
	mu sync.Mutex
	t  time.Time
}

func newJoinClock() *joinClock { return &joinClock{t: time.Now()} }

func (c *joinClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *joinClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// acceptSigs verifies every signature: the entry check tests below judge tiers and requests, and sign with
// placeholders.
type acceptSigs struct{}

func (acceptSigs) Verify(*crypto.PublicKey, *crypto.Signature, crypto.SignableTextObject) error {
	return nil
}

// keyringCrypto is the crypto module of a test node that holds the keys of keys. hidden makes it hold none, as a
// node that reaches no user key.
type keyringCrypto struct {
	cryptomod.Module
	keys   *joinKeyring
	hidden bool
}

func (c *keyringCrypto) Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	return c.keys.Sign(ctx, key, obj)
}

func (c *keyringCrypto) NewHashSigner(key *crypto.PublicKey, _ string) (cryptomod.HashSigner, error) {
	if c.hidden || c.keys.key(secp256k1.Identity(key)) == nil {
		return nil, errors.New("key not held")
	}
	return nil, nil
}

func (c *keyringCrypto) NewTextSigner(*crypto.PublicKey, string) (cryptomod.TextSigner, error) {
	return nil, errors.New("no text signer")
}

// acceptAll is the entry check of a store that accepts every entry: the view over it is the real one, and the
// entry check is out of the scope of the test.
func acceptAll(func() *astral.Identity) swarmset.Checker {
	return &scriptedChecker{script: newSwarmScript()}
}

// realRules is the entry check of the running node, with every signature accepted.
func realRules(root func() *astral.Identity) swarmset.Checker {
	return swarmset.NewRules(acceptSigs{}, root)
}

// newStoreModule returns the module of node, claimed by root, over a real open store whose entry check is
// checker(mod.rootKey), and a real view on now. It makes no typed writes: a test sets mod.writer.
func newStoreModule(t testing.TB, node, root *astral.Identity, keys *joinKeyring, now func() time.Time, checker func(func() *astral.Identity) swarmset.Checker) *Module {
	t.Helper()

	mod := claimedModule(t, root)
	mod.node = &identityNode{id: node}
	mod.log = log.New(node)
	mod.now = now
	mod.Crypto = &keyringCrypto{keys: keys}

	mod.swarmState = newSwarmStore(checker(mod.rootKey))
	ctx := astral.NewContext(nil).WithIdentity(node).IncludeZone(astral.ZoneNetwork)
	if _, err := mod.swarmState.open(ctx, newMemFiles(), node); err != nil {
		t.Fatalf("open: %v", err)
	}
	mod.swarmView = newSwarmView(mod, mod.rootKey, now)

	return mod
}

// storeMembership stores subject's membership at tier, issued by issuer and valid until expires, in mod's store.
func storeMembership(t testing.TB, mod *Module, issuer, subject *astral.Identity, tier user.Tier, expires time.Time) {
	t.Helper()

	c, err := user.NewMembershipUntil(issuer, subject, tier, nil, astral.Time(expires))
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	mergeEntry(t, mod, &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()})
}

// mergeEntry merges obj into mod's store and returns its entry ID.
func mergeEntry(t testing.TB, mod *Module, obj astral.Object) *astral.ObjectID {
	t.Helper()

	if err := mod.swarmState.merge(mod.swarmState.ctx, obj); err != nil {
		t.Fatalf("merge %T: %v", obj, err)
	}
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// countEntries returns how many entries of kind mod's store holds.
func countEntries(mod *Module, kind user.EntryKind) int {
	ids, _ := mod.SwarmEntries(kind)
	return len(ids)
}

// joinKeyring signs as the keys it holds and records the text of every object it
// signed, as a Coldcard would show it.
type joinKeyring struct {
	mu    sync.Mutex
	keys  map[string]*crypto.PrivateKey
	texts []string
}

func newJoinKeyring() *joinKeyring { return &joinKeyring{keys: map[string]*crypto.PrivateKey{}} }

// newKey adds a fresh key and returns its identity.
func (k *joinKeyring) newKey() *astral.Identity {
	key := secp256k1.New()
	id := secp256k1.Identity(secp256k1.PublicKey(key))
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[id.String()] = key
	return id
}

func (k *joinKeyring) Sign(_ *astral.Context, pub *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key, ok := k.keys[secp256k1.Identity(pub).String()]
	if !ok {
		return nil, errors.New("key not held")
	}
	k.texts = append(k.texts, obj.SignableText())
	return secp256k1sig.SignASN1(key, obj.SignableHash())
}

func (k *joinKeyring) key(id *astral.Identity) *crypto.PrivateKey {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.keys[id.String()]
}

// verifySignedBy fails t unless sig is id's signature over obj.
func verifySignedBy(t *testing.T, id *astral.Identity, obj crypto.SignableObject, sig *crypto.Signature) {
	t.Helper()
	if err := secp256k1sig.VerifyASN1(secp256k1.FromIdentity(id), obj.SignableHash(), sig); err != nil {
		t.Fatalf("signature of %v: %v", id, err)
	}
}

// answer adds an Approval (or a Decline when decline is set) of reqID signed
// by issuer, and returns its entry ID.
func answerJoin(t *testing.T, mod *Module, keys *joinKeyring, issuer *astral.Identity, reqID *astral.ObjectID, decline bool) *astral.ObjectID {
	t.Helper()
	var obj astral.Object
	if decline {
		d := user.NewDecline(issuer, reqID)
		sig, err := secp256k1sig.SignASN1(keys.key(issuer), d.SignableHash())
		if err != nil {
			t.Fatalf("sign decline: %v", err)
		}
		obj = &user.SignedDecline{Decline: d, IssuerSig: sig}
	} else {
		a := user.NewApproval(issuer, reqID)
		sig, err := secp256k1sig.SignASN1(keys.key(issuer), a.SignableHash())
		if err != nil {
			t.Fatalf("sign approval: %v", err)
		}
		obj = &user.SignedApproval{Approval: a, IssuerSig: sig}
	}
	return mergeEntry(t, mod, obj)
}
