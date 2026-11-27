package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

var _ usermod.SwarmView = &Module{}

// rootKey returns the root key: the user this node is claimed by, or this node
// while it is unclaimed.
func (mod *Module) rootKey() *astral.Identity {
	if id := mod.Identity(); !id.IsZero() {
		return id
	}
	return mod.node.Identity()
}

// wireSwarm builds the rest of SwarmState over the store and view Load built: the
// entry rules, which verify with the crypto module LoadDependencies just
// injected; the typed writes; the feed and publisher of its events; the
// carrier and the joiner.
func (mod *Module) wireSwarm() {
	mod.entryRules.SetVerifier(mod.Crypto)
	mod.writer = &swarmWriter{self: mod.node.Identity(), signer: mod.Crypto, view: mod, entry: mod.SwarmEntry, merge: mod.mergeSwarmEntry}
	mod.carrier = newSwarmCarrier(mod.node.Identity(), mod, mod, mod.Crypto)
	mod.carrier.ceremony = mod.userKeyCeremony
	mod.joiner = newSwarmJoiner(mod.node.Identity(), mod.Crypto, mod, mod.config.PendingJoin.Get())

	mod.publisher = &swarmPublisher{snapshot: mod.snapshot, emit: mod.emitSwarmEvent}
	// why: an entry this node merges is published at once, not at the feed's next poll.
	mod.swarmState.stored = func([]astral.Object) { mod.publisher.publish() }
	mod.swarmFeed = &headFeed{head: mod.SwarmHead, interval: swarmPollInterval}
}

// keepJoin keeps this node's JoinRequest in the tree, so a restart resumes
// it; nil clears it.
func (mod *Module) keepJoin(req *user.SignedJoinRequest) error {
	if req == nil {
		return mod.config.PendingJoin.Clear(mod.ctx)
	}
	return mod.config.PendingJoin.Set(mod.ctx, req)
}

// userKeyCeremony returns the ceremony in which the user key signs on this
// node, or nil when this node reaches no user key. A key held on this node
// (a software key) signs with no user action, so the ceremony waits for
// user.confirm_join; a key that only signs text (a Coldcard) asks the user on
// the device for every signature.
func (mod *Module) userKeyCeremony() *userKeyCeremony {
	userID := mod.Identity()
	if userID.IsZero() {
		return nil
	}
	key := secp256k1.FromIdentity(userID)

	ceremony := newUserKeyCeremony(userID, mod.Crypto, mod, mod)
	if _, err := mod.Crypto.NewHashSigner(key, crypto.SchemeASN1); err == nil {
		return ceremony
	}
	if _, err := mod.Crypto.NewTextSigner(key, crypto.SchemeBIP137); err == nil {
		ceremony.devicePress = true
		return ceremony
	}
	return nil
}

// runSwarmFeed reacts to every change of SwarmState the feed reports, and
// publishes its events, until ctx ends.
func (mod *Module) runSwarmFeed(ctx *astral.Context) {
	mod.publisher.follow(ctx, mod.swarmFeed, mod.onSwarmChanged)
}

// emitSwarmEvent emits e. A contract that counts is also indexed into the auth
// index.
//
// why index: the auth walk finds a caller's contracts in the auth index, so a
// contract a host signed for its app on another node authorizes it here only
// once it is indexed.
func (mod *Module) emitSwarmEvent(e astral.Object) {
	mod.Events.Emit(e)

	added, ok := e.(*user.SwarmContractAdded)
	if !ok {
		return
	}
	obj, err := mod.SwarmEntry(added.ContractID)
	if err != nil {
		return
	}
	if sc, ok := obj.(*auth.SignedContract); ok {
		if err = mod.Auth.IndexContract(mod.ctx, sc); err != nil {
			mod.log.Logv(1, "swarm state: index contract %v: %v", added.ContractID, err)
		}
	}
}

// onSwarmChanged applies what SwarmState gained: its expels, and the swarm's members.
func (mod *Module) onSwarmChanged() {
	mod.receiveSwarmExpels()

	mod.refreshBans()
	mod.publishSwarm()
}

// receiveSwarmExpels stores and applies every Expulsion SwarmState holds that
// this node has not stored yet, as a pushed one is (receiveExpulsion).
//
// why: user.list_expelled, link admission and the ban veto read the stored
// bans, and a node that learns an expel only through SwarmState (one
// introduced or joined after it) would otherwise list none and admit the
// banned node's links.
func (mod *Module) receiveSwarmExpels() {
	ids, err := mod.SwarmEntries(user.EntryExpels)
	if err != nil {
		mod.log.Logv(1, "swarm state: list expels: %v", err)
	}
	for _, id := range ids {
		obj, err := mod.SwarmEntry(id)
		if err != nil {
			continue
		}
		se, ok := obj.(*user.SignedExpulsion)
		if !ok || se.IsNil() || mod.banOn(se.Subject) != nil {
			continue
		}
		// note: a refused one (no active contract yet, another issuer) is
		// read again on the next change
		if err = mod.receiveExpulsion(se); err != nil {
			mod.log.Logv(1, "swarm state: expel %v: %v", id, err)
		}
	}
}
