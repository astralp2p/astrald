package user

import (
	"errors"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// swarmPullInterval is how often this node pulls SwarmState from every linked swarm member.
//
// why: a push is best-effort and an admit refused before a membership arrived is not retried, so the periodic
// pull is what makes every copy converge.
const swarmPullInterval = 15 * time.Second

var _ usermod.SwarmState = &Module{}

// openSwarmState opens this node's copy of SwarmState in its own VFS, which checks every stored entry again.
func (mod *Module) openSwarmState() error {
	leftOut, err := mod.swarmState.open(mod.ctx, mod.VFS, mod.node.Identity())
	if err != nil {
		return err
	}
	for _, line := range leftOut {
		mod.log.Logv(1, "swarm state: %v", line)
	}
	return nil
}

// openStore returns the store once it is open, or nil.
func (mod *Module) openStore() *swarmStore {
	if mod.swarmState == nil || !mod.swarmState.set.IsOpen() {
		return nil
	}
	return mod.swarmState
}

// admitsSwarmCaller decides who may read and write this node's SwarmState over the exchange ops: this node, the
// root key, and every identity holding a membership.
//
// why members of every tier: a Member syncs SwarmState (vision, 2026-11-24).
func (mod *Module) admitsSwarmCaller(caller *astral.Identity) bool {
	switch {
	case caller.IsZero():
		return false
	case caller.IsEqual(mod.node.Identity()):
		return true
	case caller.IsEqual(mod.Identity()):
		return true
	}
	_, ok := mod.Membership(caller)
	return ok
}

// swarmPeer returns target's exchange ops.
func (mod *Module) swarmPeer(target *astral.Identity) *routerSwarmPeer {
	return &routerSwarmPeer{router: mod.node, caller: mod.node.Identity(), target: target}
}

// swarmPeers returns the linked swarm members an accepted entry is passed on to.
func (mod *Module) swarmPeers() (peers []swarmEntryPusher) {
	for _, id := range mod.swarmMembersLinked() {
		peers = append(peers, mod.swarmPeer(id))
	}
	return peers
}

// swarmMembersLinked returns the swarm members other than this node that have a link to it.
func (mod *Module) swarmMembersLinked() (ids []*astral.Identity) {
	linked := mod.Nodes.LinkedIdentities()
	for _, id := range mod.LocalSwarm() {
		if id.IsEqual(mod.node.Identity()) || !slices.ContainsFunc(linked, id.IsEqual) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// SyncSwarm pulls the entries peer stores and this node lacks.
func (mod *Module) SyncSwarm(ctx *astral.Context, peer *astral.Identity) error {
	store := mod.openStore()
	if store == nil {
		return swarmset.ErrNotOpen
	}
	if peer.IsZero() || peer.IsEqual(mod.node.Identity()) {
		return errors.New("swarm sync needs another node")
	}

	src := mod.swarmPeer(peer)
	grown, err := store.pullSwarm(ctx.IncludeZone(astral.ZoneNetwork), src)
	if grown > 0 {
		mod.log.Logv(1, "swarm state: %v entries from %v", grown, peer)
	}
	return err
}

// runSwarmSync pulls SwarmState from every linked swarm member every swarmPullInterval until ctx ends.
func (mod *Module) runSwarmSync(ctx *astral.Context) {
	ticker := time.NewTicker(swarmPullInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, id := range mod.swarmMembersLinked() {
			if err := mod.SyncSwarm(ctx, id); err != nil {
				mod.log.Logv(2, "swarm state pull from %v: %v", id, err)
			}
		}
	}
}

// onSwarmLink exchanges SwarmState with a peer on its first link, when this node is claimed: it offers the
// peer its own membership, so the peer admits its pull, then pulls what the peer stores.
//
// why every peer of a claimed node: a peer this node does not know as a member yet may be one; its admit and
// the entry check decide what passes.
func (mod *Module) onSwarmLink(peer *astral.Identity) {
	if mod.Identity() == nil {
		return
	}
	ctx := mod.ctx.IncludeZone(astral.ZoneNetwork)
	if m, ok := mod.Membership(mod.node.Identity()); ok {
		if err := mod.swarmPeer(peer).SwarmPush(ctx, m.SignedContract); err != nil {
			mod.log.Logv(2, "swarm state: offer membership to %v: %v", peer, err)
		}
	}
	if err := mod.SyncSwarm(ctx, peer); err != nil {
		mod.log.Logv(2, "swarm state pull from %v: %v", peer, err)
	}
}

// mergeSwarmEntry offers obj to this node's SwarmState: the one merge every
// typed write ends in. When obj is this node's own membership and the store
// accepts it, the node exchanges SwarmState with every peer it holds a link
// with.
func (mod *Module) mergeSwarmEntry(ctx *astral.Context, obj astral.Object) error {
	store := mod.openStore()
	if store == nil {
		return swarmset.ErrNotOpen
	}
	before := store.set.Head()
	if err := store.merge(ctx, obj); err != nil {
		return err
	}
	if mod.isOwnMembership(obj) && store.set.Head() != before {
		go mod.exchangeSwarmWithLinked()
	}
	return nil
}

// isOwnMembership reports whether obj is a membership whose subject is this node.
func (mod *Module) isOwnMembership(obj astral.Object) bool {
	sc, ok := obj.(*auth.SignedContract)
	if !ok || sc.IsNil() || !sc.Subject.IsEqual(mod.node.Identity()) {
		return false
	}
	_, ok = user.MembershipOf(sc)
	return ok
}

// exchangeSwarmWithLinked runs the exchange of onSwarmLink with every peer
// this node holds a link with, each peer once.
//
// why: a node claimed in a ceremony (user.accept_contract) while already
// linked skipped that exchange on its links, which came up before it had a
// user, and passOn reaches no one, as its local swarm then lists only itself.
// Without this its peers never learn its membership, and it never learns
// theirs.
func (mod *Module) exchangeSwarmWithLinked() {
	for _, peer := range mod.Nodes.LinkedIdentities() {
		if peer.IsZero() {
			continue
		}
		mod.onSwarmLink(peer)
	}
}

// SwarmEntry returns a stored entry (usermod.SwarmState).
func (mod *Module) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	store := mod.openStore()
	if store == nil {
		return nil, swarmset.ErrNotOpen
	}
	return store.set.Entry(id)
}

// SwarmEntries lists the stored entries of kind (usermod.SwarmState); none before the store is open.
func (mod *Module) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	store := mod.openStore()
	if store == nil {
		return nil, nil
	}
	return store.set.Entries(kind)
}

// SwarmHead returns the number of stored entries (usermod.SwarmState); 0 before the store is open.
func (mod *Module) SwarmHead() uint64 {
	store := mod.openStore()
	if store == nil {
		return 0
	}
	return store.set.Head()
}
