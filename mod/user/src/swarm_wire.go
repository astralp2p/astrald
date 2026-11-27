package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
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

// newModuleSwarmStore returns this module's closed store, checked on every
// merge by the entry rules. Load builds it; Run opens it.
func (mod *Module) newModuleSwarmStore() *swarmStore {
	store := newSwarmStore(swarmset.NewRules(lazyVerifier{mod}, mod.rootKey), func(f string, v ...any) { mod.log.Logv(1, f, v...) })
	store.peers = mod.swarmPeers
	return store
}

// lazyVerifier verifies with the crypto module, resolved at each check.
//
// why lazy: Load builds the store before LoadDependencies injects the crypto
// module.
type lazyVerifier struct{ mod *Module }

func (v lazyVerifier) Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error {
	return v.mod.Crypto.Verify(key, sig, obj)
}

// wireSwarm builds the rest of SwarmState over the store Load built: the
// SwarmView, which the auth walk reads through its dependency on this module;
// the typed writes; the feed and publisher of its events; and the answering
// and carrying ops.
func (mod *Module) wireSwarm() {
	view := newSwarmView(mod, mod.rootKey, nil)
	mod.swarmView.Store(view)
	mod.writer = &swarmWriter{self: mod.node.Identity(), signer: mod.Crypto, view: mod, entry: mod.SwarmEntry, merge: mod.mergeSwarmEntry}
	mod.carrier = newSwarmCarrier(mod.node.Identity(), mod, mod, mod.Crypto)
	mod.joiner = newSwarmJoiner(mod.node.Identity(), mod.Crypto, mod, mod.config.PendingJoin.Get())

	mod.publisher = newSwarmPublisher(view.snapshot, func(e astral.Object) { mod.Events.Emit(e) })
	// why: an entry this node merges is published at once, not at the feed's next poll.
	mod.swarm.stored = func([]astral.Object) { mod.publisher.publish() }
	mod.swarmFeed = newHeadFeed(mod, swarmPollInterval)

	mod.router.AddStructPrefix(&SwarmAnswerOps{
		Node:      mod.node.Identity(),
		State:     mod,
		View:      mod,
		Auth:      mod.Auth,
		UserKey:   mod.userKeyCeremony,
		Confirmed: mod.carrier.confirmed,
		AsUser:    func(uk *userKeyCeremony) answerer { return mod.writer.as(uk.user, uk.signer) },
	}, "Op")
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

// onSwarmChanged indexes the contracts SwarmState gained, and re-reads the
// swarm's members.
//
// why index: the auth walk finds a caller's contracts in the auth index, so a
// contract a host signed for its app on another node authorizes it here only
// once it is indexed.
func (mod *Module) onSwarmChanged() {
	ids, err := mod.SwarmEntries(user.EntryContracts)
	if err != nil {
		mod.log.Logv(1, "swarm state: list contracts: %v", err)
	}
	for _, id := range ids {
		if _, done := mod.indexedEntries.Get(*id); done {
			continue
		}
		obj, err := mod.SwarmEntry(id)
		if err != nil {
			continue
		}
		sc, ok := obj.(*auth.SignedContract)
		if !ok {
			continue
		}
		// why: an offer grants nothing, and auth indexes only contracts both
		// parties signed; its completed form is another entry.
		if swarmset.IsOffer(sc) {
			mod.indexedEntries.Set(*id, struct{}{})
			continue
		}
		if err = mod.Auth.IndexContract(mod.ctx, sc); err != nil {
			mod.log.Logv(1, "swarm state: index contract %v: %v", id, err)
		}
		mod.indexedEntries.Set(*id, struct{}{})
	}

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
		if _, done := mod.indexedEntries.Get(*id); done {
			continue
		}
		obj, err := mod.SwarmEntry(id)
		if err != nil {
			continue
		}
		se, ok := obj.(*user.SignedExpulsion)
		if !ok || se.IsNil() {
			continue
		}
		// note: a refused one (no active contract yet, another issuer) is
		// read again on the next change
		if mod.banOn(se.Subject) == nil {
			if err = mod.receiveExpulsion(se); err != nil {
				mod.log.Logv(1, "swarm state: expel %v: %v", id, err)
				continue
			}
		}
		mod.indexedEntries.Set(*id, struct{}{})
	}
}

// swarmMembers returns every identity whose membership holds in SwarmState;
// none before LoadDependencies built the view.
func (mod *Module) swarmMembers() []*astral.Identity {
	view := mod.swarmView.Load()
	if view == nil {
		return nil
	}
	return view.members()
}

// RequestStatus reads the view (usermod.SwarmView); pending before
// LoadDependencies built it.
func (mod *Module) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	view := mod.swarmView.Load()
	if view == nil {
		return user.RequestPending
	}
	return view.RequestStatus(req)
}

// Expelled reads the view (usermod.SwarmView); false before LoadDependencies built it.
func (mod *Module) Expelled(id *astral.Identity) bool {
	view := mod.swarmView.Load()
	if view == nil {
		return false
	}
	return view.Expelled(id)
}

// ApprovalsTouching reads the view (usermod.SwarmView); none before LoadDependencies built it.
func (mod *Module) ApprovalsTouching(node *astral.Identity) []*user.SignedApproval {
	view := mod.swarmView.Load()
	if view == nil {
		return nil
	}
	return view.ApprovalsTouching(node)
}

// ContractsOf reads the view (usermod.SwarmView); none before LoadDependencies built it.
func (mod *Module) ContractsOf(req *astral.ObjectID) []*auth.SignedContract {
	view := mod.swarmView.Load()
	if view == nil {
		return nil
	}
	return view.ContractsOf(req)
}

func (mod *Module) Membership(id *astral.Identity) (*user.Membership, bool) {
	view := mod.swarmView.Load()
	if view == nil {
		return nil, false
	}
	if m, ok := view.Membership(id); ok {
		return m, true
	}
	return mod.ownMembership(id)
}

// ownMembership views this node's active contract as its membership, until
// SwarmState holds it.
//
// why: a claim or a join makes the contract active before
// onActiveContractChanged adds it to SwarmState, and in that window the node
// would be neither its own root nor a member: a token-less user.info right
// after user.bootstrap was refused.
func (mod *Module) ownMembership(id *astral.Identity) (*user.Membership, bool) {
	if id.IsZero() || !id.IsEqual(mod.node.Identity()) || mod.Expelled(id) {
		return nil, false
	}
	ac := mod.ActiveContract()
	if ac == nil || !time.Now().Before(ac.ExpiresAt.Time()) {
		return nil, false
	}
	return user.MembershipOf(ac)
}

// MayAnswer reports whether signer may answer the stored request req
// (usermod.SwarmView), reading signer's tier through Membership, so this
// node's active contract counts until SwarmState holds it.
func (mod *Module) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	view := mod.swarmView.Load()
	if view == nil {
		return false
	}
	r, ok := view.snapshot().Request(req)
	return ok && swarmset.MayAnswer(mod.rootKey(), signer, r.Obj, mod.Membership)
}
