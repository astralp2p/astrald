package user

import (
	"errors"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// membershipReader is the part of usermod.SwarmView that reads an identity's
// tier.
type membershipReader interface {
	Membership(id *astral.Identity) (*user.Membership, bool)
}

// swarmReader is the reading part of usermod.SwarmState the view reads.
type swarmReader interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
	SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error)
	SwarmHead() uint64
}

// swarmView implements the reading part of usermod.SwarmView over the entries
// it reads through usermod.SwarmState. Every answer comes from a
// swarmset.Snapshot, which is pure: the set as read, the root key and the
// time of the call.
type swarmView struct {
	state swarmReader
	root  func() *astral.Identity
	now   func() time.Time

	mu   sync.Mutex
	head uint64
	set  *swarmset.Index
}

// newSwarmView returns a view of state. root returns the root key: the user
// this node is claimed by, or the node itself while unclaimed. A nil now
// selects time.Now.
func newSwarmView(state swarmReader, root func() *astral.Identity, now func() time.Time) *swarmView {
	if now == nil {
		now = time.Now
	}
	return &swarmView{state: state, root: root, now: now}
}

func (v *swarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	return v.snapshot().RequestStatus(req)
}

func (v *swarmView) Expelled(id *astral.Identity) bool {
	return v.snapshot().Expelled(id)
}

func (v *swarmView) ContractsOf(req *astral.ObjectID) []*auth.SignedContract {
	return v.snapshot().ContractsOf(req)
}

func (v *swarmView) Membership(id *astral.Identity) (*user.Membership, bool) {
	return v.snapshot().Membership(id)
}

func (v *swarmView) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	return v.snapshot().MayAnswer(signer, req)
}

// members returns every identity whose membership holds, in no defined order.
func (v *swarmView) members() []*astral.Identity {
	return v.snapshot().Members()
}

// snapshot returns the set as of the current SwarmHead, read again only when
// the head has grown since the last read.
func (v *swarmView) snapshot() *swarmset.Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()

	// why: the head is read before the entries, so a set read while entries
	// arrive is at least as new as its head, and the next grown head re-reads it.
	head := v.state.SwarmHead()
	if v.set == nil || head != v.head {
		set, err := readSwarmIndex(v.state)
		if err != nil {
			// why: the set only grows, so a failed read must not drop entries
			// an earlier read held, or a transient error would undo an expel
			// or a Decline. The head stays, so the next call reads again.
			v.set = set.Union(v.set)
			return v.snapshotOf(v.set)
		}
		v.set, v.head = set, head
	}
	return v.snapshotOf(v.set)
}

func (v *swarmView) snapshotOf(set *swarmset.Index) *swarmset.Snapshot {
	return &swarmset.Snapshot{Index: set, Root: v.root(), Now: v.now()}
}

// readSwarmIndex reads every stored entry of state. On a read error it
// returns what it read and the joined errors.
func readSwarmIndex(state swarmReader) (*swarmset.Index, error) {
	set := swarmset.NewIndex()
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
			set.Add(id, obj)
		}
	}
	return set, errors.Join(errs...)
}
