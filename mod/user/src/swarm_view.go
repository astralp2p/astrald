package user

import (
	"errors"
	usermod "github.com/astralp2p/astrald/mod/user"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// swarmView implements the reading part of usermod.SwarmView over the entries
// it reads through usermod.SwarmState. Every answer comes from a swarmSnapshot,
// which is pure: the set as read, the root key and the time of the call.
//
// note: CheckSwarmEntry is G3's; G7 joins both into Module and wires the view
// to Deps.Nodes. Nothing in the running node constructs a swarmView yet.
type swarmView struct {
	state usermod.SwarmState
	root  func() *astral.Identity
	now   func() time.Time

	mu   sync.Mutex
	head uint64
	set  *swarmSet
}

// newSwarmView returns a view of state. root returns the root key: the user
// this node is claimed by, or the node itself while unclaimed. A nil now
// selects time.Now.
func newSwarmView(state usermod.SwarmState, root func() *astral.Identity, now func() time.Time) *swarmView {
	if now == nil {
		now = time.Now
	}
	return &swarmView{state: state, root: root, now: now}
}

func (v *swarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	return v.snapshot().requestStatus(req)
}

func (v *swarmView) Expelled(id *astral.Identity) bool {
	return v.snapshot().expelled(id)
}

func (v *swarmView) ApprovalsTouching(node *astral.Identity) []*user.SignedApproval {
	return v.snapshot().approvalsTouching(node)
}

func (v *swarmView) CarriedOut(approval *astral.ObjectID) []*auth.SignedContract {
	return v.snapshot().carriedOut(approval)
}

func (v *swarmView) Membership(id *astral.Identity) (*user.Membership, bool) {
	return v.snapshot().membership(id, map[string]bool{})
}

// ApprovedNotCarriedOut reports whether req is approved while no contract
// carries out any of its Approvals yet: the touched node has not polled since,
// or is offline. Monitoring shows it as "approved, not yet carried out".
func (v *swarmView) ApprovedNotCarriedOut(req *astral.ObjectID) bool {
	return v.snapshot().approvedNotCarriedOut(req)
}

// snapshot returns the set as of the current SwarmHead, read again only when
// the head has grown since the last read.
func (v *swarmView) snapshot() *swarmSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()

	// why: the head is read before the entries, so a set read while entries
	// arrive is at least as new as its head, and the next grown head re-reads it.
	head := v.state.SwarmHead()
	if v.set == nil || head != v.head {
		set, err := readSwarmSet(v.state)
		if err != nil {
			// why: the set only grows, so a failed read must not drop entries
			// an earlier read held, or a transient error would undo an expel
			// or a Decline. The head stays, so the next call reads again.
			v.set = set.union(v.set)
			return v.snapshotOf(v.set)
		}
		v.set, v.head = set, head
	}
	return v.snapshotOf(v.set)
}

func (v *swarmView) snapshotOf(set *swarmSet) *swarmSnapshot {
	return &swarmSnapshot{swarmSet: set, root: v.root(), now: v.now()}
}

// swarmSet is one read of SwarmState, keyed by entry ID.
type swarmSet struct {
	entries map[astral.ObjectID]astral.Object

	// requests holds *user.SignedContractRequest, *user.SignedJoinRequest and
	// *user.SignedExpelRequest.
	requests  map[astral.ObjectID]astral.Object
	approvals map[astral.ObjectID]*user.SignedApproval
	declines  []*user.SignedDecline
	contracts []*auth.SignedContract
	expels    []*user.SignedExpulsion
}

// readSwarmSet reads every stored entry of state. On a read error it returns
// what it read and the joined errors.
func readSwarmSet(state usermod.SwarmState) (*swarmSet, error) {
	set := newSwarmSet()
	var errs []error
	for _, kind := range user.EntryKinds {
		ids, err := state.SwarmEntries(kind)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, id := range ids {
			obj, err := state.SwarmEntry(id)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			set.add(id, obj)
		}
	}
	return set, errors.Join(errs...)
}

func newSwarmSet() *swarmSet {
	return &swarmSet{
		entries:   map[astral.ObjectID]astral.Object{},
		requests:  map[astral.ObjectID]astral.Object{},
		approvals: map[astral.ObjectID]*user.SignedApproval{},
	}
}

// union returns a new set holding the entries of set and of other; either
// may be nil.
func (set *swarmSet) union(other *swarmSet) *swarmSet {
	out := newSwarmSet()
	for _, s := range []*swarmSet{set, other} {
		if s == nil {
			continue
		}
		for id, obj := range s.entries {
			out.add(&id, obj)
		}
	}
	return out
}

// add files obj under its kind; an object of no entry kind, or an entry
// already held, is ignored.
func (set *swarmSet) add(id *astral.ObjectID, obj astral.Object) {
	if id == nil {
		return
	}
	if _, held := set.entries[*id]; held {
		return
	}
	if _, ok := user.EntryKindOf(obj); !ok {
		return
	}
	set.entries[*id] = obj
	switch e := obj.(type) {
	case *user.SignedContractRequest, *user.SignedJoinRequest, *user.SignedExpelRequest:
		set.requests[*id] = e
	case *user.SignedApproval:
		set.approvals[*id] = e
	case *user.SignedDecline:
		set.declines = append(set.declines, e)
	case *auth.SignedContract:
		set.contracts = append(set.contracts, e)
	case *user.SignedExpulsion:
		set.expels = append(set.expels, e)
	}
}

// swarmSnapshot reads a swarmSet as of now, with root as the root key. Its
// methods are pure functions of these three.
//
// note: stored entries passed CheckSwarmEntry on merge, so signatures and an
// answer's right to answer (user.MayAnswer) are trusted. What changes after
// merge is applied here: Timeouts, expiry, Declines and expels.
type swarmSnapshot struct {
	*swarmSet
	root *astral.Identity
	now  time.Time
}

func (s *swarmSnapshot) isRoot(id *astral.Identity) bool {
	return !s.root.IsZero() && s.root.IsEqual(id)
}

// expelled reports whether a stored Expulsion names id as its Subject.
func (s *swarmSnapshot) expelled(id *astral.Identity) bool {
	if id.IsZero() {
		return false
	}
	for _, e := range s.expels {
		if !e.IsNil() && e.Subject.IsEqual(id) {
			return true
		}
	}
	return false
}

// counts reports whether an answer or contract signed by signer counts: an
// expel beats everything signed by the expelled identity.
func (s *swarmSnapshot) counts(signer *astral.Identity) bool {
	return !signer.IsZero() && !s.expelled(signer)
}
