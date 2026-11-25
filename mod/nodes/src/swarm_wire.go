package nodes

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// swarmPullInterval is how often this node pulls SwarmState from every linked swarm member.
//
// why: a push is best-effort and an admit refused before a membership arrived is not retried, so the periodic
// pull is what makes every copy converge.
const swarmPullInterval = 15 * time.Second

// errSwarmStateNotOpen answers a SwarmState call made before Run opened the store.
var errSwarmStateNotOpen = errors.New("swarm state is not open yet")

var _ nodesmod.SwarmState = &Module{}

// openSwarmState opens this node's copy of SwarmState in its own VFS, checked on merge by the user module's
// SwarmView, and adds the exchange ops to the module's router.
func (mod *Module) openSwarmState() error {
	if mod.VFS == nil || mod.User == nil {
		return errors.New("swarm state needs the vfs and user modules")
	}
	store, err := newSwarmStore(mod.ctx, mod.VFS, mod.node.Identity(), mod.User)
	if err != nil {
		return err
	}
	store.peers = mod.swarmPeers

	if err = addSwarmOps(&mod.router, store, mod.admitsSwarmCaller); err != nil {
		return err
	}

	mod.swarm.Store(store)
	return nil
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
	case caller.IsEqual(mod.User.Identity()):
		return true
	}
	_, ok := mod.User.Membership(caller)
	return ok
}

// swarmPeers returns the linked swarm members an accepted entry is passed on to.
func (mod *Module) swarmPeers() (peers []swarmPusher) {
	for _, id := range mod.swarmMembersLinked() {
		peers = append(peers, &routerSwarmPeer{router: mod.node, caller: mod.node.Identity(), target: id})
	}
	return peers
}

// swarmMembersLinked returns the swarm members other than this node that have a link to it.
func (mod *Module) swarmMembersLinked() (ids []*astral.Identity) {
	for _, id := range mod.User.LocalSwarm() {
		if id.IsEqual(mod.node.Identity()) || !mod.IsLinked(id) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// SyncSwarm pulls the entries peer stores and this node lacks.
func (mod *Module) SyncSwarm(ctx *astral.Context, peer *astral.Identity) error {
	store := mod.swarm.Load()
	if store == nil {
		return errSwarmStateNotOpen
	}
	if peer.IsZero() || peer.IsEqual(mod.node.Identity()) {
		return errors.New("swarm sync needs another node")
	}

	src := &routerSwarmPeer{router: mod.node, caller: mod.node.Identity(), target: peer}
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
// CheckSwarmEntry decide what passes.
func (mod *Module) onSwarmLink(peer *astral.Identity) {
	if mod.User == nil || mod.User.Identity() == nil {
		return
	}
	ctx := mod.ctx.IncludeZone(astral.ZoneNetwork)
	if m, ok := mod.User.Membership(mod.node.Identity()); ok {
		own := &routerSwarmPeer{router: mod.node, caller: mod.node.Identity(), target: peer}
		if err := own.SwarmPush(ctx, m.SignedContract); err != nil {
			mod.log.Logv(2, "swarm state: offer membership to %v: %v", peer, err)
		}
	}
	if err := mod.SyncSwarm(ctx, peer); err != nil {
		mod.log.Logv(2, "swarm state pull from %v: %v", peer, err)
	}
}

// AddSwarmEntry offers obj to this node's SwarmState (nodesmod.SwarmState).
func (mod *Module) AddSwarmEntry(ctx *astral.Context, obj astral.Object) error {
	store := mod.swarm.Load()
	if store == nil {
		return errSwarmStateNotOpen
	}
	return store.AddSwarmEntry(ctx, obj)
}

// SwarmEntry returns a stored entry (nodesmod.SwarmState).
func (mod *Module) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	store := mod.swarm.Load()
	if store == nil {
		return nil, errSwarmStateNotOpen
	}
	return store.SwarmEntry(id)
}

// SwarmEntries lists the stored entries of kind (nodesmod.SwarmState).
func (mod *Module) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	store := mod.swarm.Load()
	if store == nil {
		return nil, nil
	}
	return store.SwarmEntries(kind)
}

// SwarmHead returns the number of stored entries (nodesmod.SwarmState); 0 before the store is open.
func (mod *Module) SwarmHead() uint64 {
	store := mod.swarm.Load()
	if store == nil {
		return 0
	}
	return store.SwarmHead()
}
