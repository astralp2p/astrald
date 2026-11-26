package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
)

var _ usermod.SwarmView = &Module{}

// wireSwarm builds the SwarmView over the nodes module's SwarmState, hands it
// to the auth walk, and adds the answering and carrying ops.
func (mod *Module) wireSwarm() {
	mod.swarmView = newSwarmView(mod.Nodes, mod.rootKey, nil)
	mod.entryCheck = newSwarmEntryCheck(mod.Nodes, mod.swarmView, mod.Crypto, mod.rootKey)
	mod.carrier = newSwarmCarrier(mod.node.Identity(), mod.Nodes, mod, mod.Crypto)
	mod.joiner = newSwarmJoiner(mod.node.Identity(), mod.Crypto, mod, mod.config.PendingJoin.Get())

	mod.Auth.SetSwarm(mod, mod.Nodes, mod.rootKey)

	mod.router.AddStructPrefix(&SwarmAnswerOps{
		Node:    mod.node.Identity(),
		State:   mod.Nodes,
		View:    mod,
		Auth:    mod.Auth,
		Signer:  nodeSigner{mod.Crypto},
		UserKey: mod.userKeyCeremony,
		Root:    mod.rootKey,
	}, "Op")
}

// nodeSigner signs with the node key, resolving the signer at each signature.
//
// why lazy: wireSwarm runs in LoadDependencies, while the crypto engines
// still register; NodeSigner read then races with them and panics without one.
type nodeSigner struct {
	crypto interface{ NodeSigner() cryptomod.HashSigner }
}

func (s nodeSigner) SignHash(ctx *astral.Context, hash []byte) (*crypto.Signature, error) {
	return s.crypto.NodeSigner().SignHash(ctx, hash)
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

	ceremony := newUserKeyCeremony(userID, mod.Crypto, mod.Nodes, mod)
	if _, err := mod.Crypto.NewHashSigner(key, crypto.SchemeASN1); err == nil {
		return ceremony
	}
	if _, err := mod.Crypto.NewTextSigner(key, crypto.SchemeBIP137); err == nil {
		ceremony.devicePress = true
		return ceremony
	}
	return nil
}

// runSwarmWatcher reacts to every change of SwarmState until ctx ends.
func (mod *Module) runSwarmWatcher(ctx *astral.Context) {
	_ = newSwarmWatcher(mod.Nodes, 0, func(uint64) { mod.onSwarmChanged() }).Run(ctx)
}

// onSwarmChanged indexes the contracts SwarmState gained, and re-reads the
// swarm's members.
//
// why index: the auth walk finds a caller's contracts in the auth index, so a
// contract a host signed for its app on another node authorizes it here only
// once it is indexed.
func (mod *Module) onSwarmChanged() {
	ids, err := mod.Nodes.SwarmEntries(user.EntryContracts)
	if err != nil {
		mod.log.Logv(1, "swarm state: list contracts: %v", err)
	}
	for _, id := range ids {
		if _, done := mod.indexedEntries.Get(*id); done {
			continue
		}
		obj, err := mod.Nodes.SwarmEntry(id)
		if err != nil {
			continue
		}
		sc, ok := obj.(*auth.SignedContract)
		if !ok {
			continue
		}
		if err = mod.Auth.IndexContract(mod.ctx, sc); err != nil {
			mod.log.Logv(1, "swarm state: index contract %v: %v", id, err)
		}
		mod.indexedEntries.Set(*id, struct{}{})
	}

	mod.refreshBans()
	mod.publishSwarm()
	mod.runSiblingLinker()
}

// swarmMembers returns every identity whose membership holds in SwarmState;
// none before LoadDependencies built the view.
func (mod *Module) swarmMembers() []*astral.Identity {
	if mod.swarmView == nil {
		return nil
	}
	return mod.swarmView.members()
}

func (mod *Module) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	return mod.swarmView.RequestStatus(req)
}

func (mod *Module) Expelled(id *astral.Identity) bool {
	return mod.swarmView.Expelled(id)
}

func (mod *Module) ApprovalsTouching(node *astral.Identity) []*user.SignedApproval {
	return mod.swarmView.ApprovalsTouching(node)
}

func (mod *Module) CarriedOut(approval *astral.ObjectID) []*auth.SignedContract {
	return mod.swarmView.CarriedOut(approval)
}

func (mod *Module) Membership(id *astral.Identity) (*user.Membership, bool) {
	if mod.swarmView == nil {
		return nil, false
	}
	if m, ok := mod.swarmView.Membership(id); ok {
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
	if id.IsZero() || !id.IsEqual(mod.node.Identity()) || mod.swarmView.Expelled(id) {
		return nil, false
	}
	ac := mod.ActiveContract()
	if ac == nil || !time.Now().Before(ac.ExpiresAt.Time()) {
		return nil, false
	}
	return user.MembershipOf(ac)
}

func (mod *Module) CheckSwarmEntry(obj astral.Object) user.EntryVerdict {
	return mod.entryCheck.CheckSwarmEntry(obj)
}
