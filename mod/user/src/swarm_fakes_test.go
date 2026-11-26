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
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	usermod "github.com/astralp2p/astrald/mod/user"
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

// joinSwarm is a fake of user.SwarmState and user.SwarmView over one
// in-memory entry set. It accepts every entry kind and derives answers the
// way the SwarmView contract states them, with memberships set by the test.
type joinSwarm struct {
	mu          sync.Mutex
	clock       *joinClock
	entries     map[string]astral.Object
	order       []string
	memberships map[string]*user.Membership
}

var (
	_ carrierState      = &joinSwarm{}
	_ ceremonyState     = &joinSwarm{}
	_ usermod.SwarmView = &joinSwarm{}
)

func newJoinSwarm(clock *joinClock) *joinSwarm {
	return &joinSwarm{clock: clock, entries: map[string]astral.Object{}, memberships: map[string]*user.Membership{}}
}

// setTier gives id a membership at tier, issued by issuer.
func (s *joinSwarm) setTier(issuer, id *astral.Identity, tier user.Tier) {
	contract, _ := user.NewMembership(issuer, id, tier, nil, 24*time.Hour)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memberships[id.String()] = &user.Membership{SignedContract: &auth.SignedContract{Contract: contract}, Tier: tier}
}

func (s *joinSwarm) merge(_ *astral.Context, obj astral.Object) error {
	if _, ok := user.EntryKindOf(obj); !ok {
		return errors.New("not a swarm entry")
	}
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id.String()]; !ok {
		s.entries[id.String()] = obj
		s.order = append(s.order, id.String())
	}
	return nil
}

// FileJoinRequest, AddContract and AddExpulsion are the typed writes the
// carrier and the ceremony make; each merges its entry.
func (s *joinSwarm) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	id, err := astral.ResolveObjectID(req)
	if err != nil {
		return nil, err
	}
	return id, s.merge(ctx, req)
}

func (s *joinSwarm) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	return s.merge(ctx, sc)
}

func (s *joinSwarm) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	return s.merge(ctx, e)
}

func (s *joinSwarm) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.entries[id.String()]
	if !ok {
		return nil, errors.New("not stored")
	}
	return obj, nil
}

func (s *joinSwarm) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	var ids []*astral.ObjectID
	for _, obj := range s.all() {
		if k, _ := user.EntryKindOf(obj); k == kind {
			id, _ := astral.ResolveObjectID(obj)
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *joinSwarm) SwarmHead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.entries))
}

func (s *joinSwarm) all() []astral.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	objs := make([]astral.Object, 0, len(s.order))
	for _, id := range s.order {
		objs = append(objs, s.entries[id])
	}
	return objs
}

func (s *joinSwarm) count(kind user.EntryKind) int {
	ids, _ := s.SwarmEntries(kind)
	return len(ids)
}

func (s *joinSwarm) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	status := user.RequestPending
	for _, obj := range s.all() {
		switch e := obj.(type) {
		case *user.SignedDecline:
			if e.RequestID.IsEqual(req) {
				return user.RequestDeclined
			}
		case *user.SignedApproval:
			if e.RequestID.IsEqual(req) {
				status = user.RequestApproved
			}
		case *user.SignedExpulsion:
			if r, ok := s.entryOf(req).(*user.SignedExpelRequest); ok && r.Subject.IsEqual(e.Subject) {
				status = user.RequestApproved
			}
		}
	}
	if status == user.RequestPending && fakeRequestExpired(s.entryOf(req), s.clock.now()) {
		return user.RequestExpired
	}
	return status
}

func fakeRequestExpired(obj astral.Object, now time.Time) bool {
	switch r := obj.(type) {
	case *user.SignedJoinRequest:
		return r.Expired(now)
	case *user.SignedExpelRequest:
		return r.Expired(now)
	}
	return false
}

func (s *joinSwarm) entryOf(id *astral.ObjectID) astral.Object {
	obj, _ := s.SwarmEntry(id)
	return obj
}

func (s *joinSwarm) Expelled(id *astral.Identity) bool {
	for _, obj := range s.all() {
		if e, ok := obj.(*user.SignedExpulsion); ok && e.Subject.IsEqual(id) {
			return true
		}
	}
	return false
}

func (s *joinSwarm) ApprovalsTouching(node *astral.Identity) []*user.SignedApproval {
	var list []*user.SignedApproval
	for _, obj := range s.all() {
		a, ok := obj.(*user.SignedApproval)
		if !ok {
			continue
		}
		if r, ok := s.entryOf(a.RequestID).(*user.SignedJoinRequest); ok && (r.Carrier.IsEqual(node) || r.Joiner.IsEqual(node)) {
			list = append(list, a)
		}
	}
	return list
}

func (s *joinSwarm) CarriedOut(approval *astral.ObjectID) []*auth.SignedContract {
	var list []*auth.SignedContract
	for _, obj := range s.all() {
		if sc, ok := obj.(*auth.SignedContract); ok && sc.ApprovalID.IsEqual(approval) {
			list = append(list, sc)
		}
	}
	return list
}

func (s *joinSwarm) Membership(id *astral.Identity) (*user.Membership, bool) {
	s.mu.Lock()
	m, ok := s.memberships[id.String()]
	s.mu.Unlock()
	if ok {
		return m, true
	}
	for _, obj := range s.all() {
		if sc, ok := obj.(*auth.SignedContract); ok && sc.Subject.IsEqual(id) {
			if m, ok := user.MembershipOf(sc); ok {
				return m, true
			}
		}
	}
	return nil, false
}

func (s *joinSwarm) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	m, ok := s.Membership(signer)
	obj := s.entryOf(req)
	return ok && obj != nil && user.MayAnswer(m.Tier, obj)
}

// writeState is a SwarmState over a fake store: the store's reads and the
// typed writes of a real swarmWriter, whose merge is the store's.
type writeState struct {
	swarmReader
	*swarmWriter
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
func answerJoin(t *testing.T, s *joinSwarm, keys *joinKeyring, issuer *astral.Identity, reqID *astral.ObjectID, decline bool) *astral.ObjectID {
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
	if err := s.merge(nil, obj); err != nil {
		t.Fatalf("add answer: %v", err)
	}
	id, _ := astral.ResolveObjectID(obj)
	return id
}

// awaitSwarmEntries waits until s holds n entries of kind.
func awaitSwarmEntries(t *testing.T, s *joinSwarm, kind user.EntryKind, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.count(kind) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d %v", n, kind)
		}
		time.Sleep(time.Millisecond)
	}
}
