package user

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// membershipReader is the part of usermod.SwarmView that reads an identity's
// tier.
type membershipReader interface {
	Membership(id *astral.Identity) (*user.Membership, bool)
}

// swarmView implements the reading part of usermod.SwarmView over the entries
// it reads through usermod.SwarmState. Every answer comes from a
// swarmset.Snapshot, which is pure: the set as read, the root key and the
// time of the call.
type swarmView struct {
	state usermod.SwarmState
	root  func() *astral.Identity
	now   func() time.Time

	mu   sync.Mutex
	head uint64
	set  *swarmset.Index
}

// newSwarmView returns a view of state. root returns the root key: the user
// this node is claimed by, or the node itself while unclaimed. now is the
// clock of expiry.
func newSwarmView(state usermod.SwarmState, root func() *astral.Identity, now func() time.Time) *swarmView {
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

// snapshot returns the set as of the current SwarmHead, read again only when
// the head has grown since the last read.
//
// why: the head is read before the entries, so a set read while entries
// arrive is at least as new as its head, and the next grown head re-reads it.
// A failed read keeps the last set read and the head it was read at, so the
// next call reads again.
func (v *swarmView) snapshot() *swarmset.Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()

	head := v.state.SwarmHead()
	if v.set == nil || head != v.head {
		if set, err := readSwarmIndex(v.state); err == nil {
			v.set, v.head = set, head
		} else if v.set == nil {
			v.set = swarmset.NewIndex()
		}
	}
	return &swarmset.Snapshot{Index: v.set, Root: v.root(), Now: v.now()}
}

// readSwarmIndex reads every stored entry of state.
func readSwarmIndex(state usermod.SwarmState) (*swarmset.Index, error) {
	set := swarmset.NewIndex()
	for _, kind := range user.EntryKinds {
		ids, err := state.SwarmEntries(kind)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			obj, err := state.SwarmEntry(id)
			if err != nil {
				return nil, err
			}
			set.Add(id, obj)
		}
	}
	return set, nil
}
