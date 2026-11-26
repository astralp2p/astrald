package user

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// validateActiveContract enforces the invariant of the active-contract slot:
// the issuer's signature is valid, and the subject's when present, this node
// is the subject and not the issuer, not expired, and it is a membership (one
// tier permit). Source-independent - the same gate for a bootstrap, a
// Coldcard ceremony and a membership a carrier handed over.
//
// why the subject signature is optional: a carrier signs a joiner's
// membership alone, as SwarmState admits it (user.SwarmView.Membership).
func (mod *Module) validateActiveContract(signed *auth.SignedContract) error {
	if signed.IsNil() {
		return errors.New("no contract")
	}
	if err := mod.Auth.VerifyIssuer(signed); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if signed.SubjectSig != nil {
		if err := mod.Auth.VerifySubject(signed); err != nil {
			return fmt.Errorf("verify: %w", err)
		}
	}
	if !signed.Subject.IsEqual(mod.node.Identity()) {
		return errors.New("local node is not the subject of the contract")
	}
	if signed.Issuer.IsEqual(signed.Subject) {
		return errors.New("local node is the issuer of the contract")
	}
	if signed.ExpiresAt.Time().Before(time.Now()) {
		return auth.ErrContractExpired
	}
	if _, ok := user.ContractTier(signed.Contract); !ok {
		return errors.New("contract is not a membership")
	}
	return nil
}

// validateClaimContract is validateActiveContract for a contract the user key
// signed for this node in a ceremony: a PrivilegedMembership carrying this
// node's own subject signature.
func (mod *Module) validateClaimContract(signed *auth.SignedContract) error {
	if err := mod.validateActiveContract(signed); err != nil {
		return err
	}
	if tier, _ := user.ContractTier(signed.Contract); tier != user.TierPrivilegedMember {
		return errors.New("contract is not a privileged membership")
	}
	if signed.SubjectSig == nil {
		return errors.New("contract lacks this node's signature")
	}
	return nil
}

// SetActiveContract writes the contract to the durable tree store. It validates
// up front for a synchronous error and to keep an invalid contract out of the
// store; the store is the source of truth and onActiveContractChanged reacts.
func (mod *Module) SetActiveContract(signed *auth.SignedContract) error {
	err := mod.validateActiveContract(signed)
	if err != nil {
		return err
	}

	return mod.config.ActiveContract.Set(mod.ctx, signed)
}

// errClaimed answers a claim that another claim has preceded.
var errClaimed = errors.New("the node has an active contract")

// claim stores signed and sets it as the active contract, unless the node has
// one already.
//
// why mu: the check and the set are one transition; two claims would each pass
// an unlocked check, and the second set would replace the first.
func (mod *Module) claim(ctx *astral.Context, signed *auth.SignedContract) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if mod.ActiveContract() != nil {
		return errClaimed
	}

	_, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	if err != nil {
		return err
	}

	return mod.SetActiveContract(signed)
}

// ActiveContract returns the active contract from the tree store - the single
// source of truth. Nil when the node is unclaimed.
func (mod *Module) ActiveContract() *auth.SignedContract {
	return mod.config.ActiveContract.Get()
}

// Identity returns the user identity, not the local node identity: the
// stored user when a carrier signed this node's membership, otherwise the
// issuer of the active contract. Nil when the node is unclaimed.
func (mod *Module) Identity() *astral.Identity {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil
	}
	if u := mod.config.User.Get(); !u.IsZero() {
		return u
	}
	return ac.Issuer
}

// onActiveContractChanged reacts to a change of the active-contract store,
// driven by Follow at startup and on every write. It keeps no copy - readers
// use ActiveContract(). A stored value that fails validation (stale, corrupt,
// or a stray write) is scrubbed so it never stays live.
func (mod *Module) onActiveContractChanged(signed *auth.SignedContract) {
	defer mod.publishSwarm()
	defer mod.refreshBans()
	if signed.IsNil() {
		mod.log.Info("this node runs without a user")
		if !mod.config.User.Get().IsZero() {
			_ = mod.config.User.Clear(mod.ctx)
		}
		// why linkerMu: a linker run that read the old swarm before the clear
		// would otherwise register tasks for former members after this prune.
		mod.linkerMu.Lock()
		mod.pruneSiblings(nil)
		mod.linkerMu.Unlock()
		return
	}

	err := mod.validateActiveContract(signed)
	if err != nil {
		mod.log.Error("invalid active contract in store, clearing: %v", err)
		clearErr := mod.config.ActiveContract.Clear(mod.ctx)
		if clearErr != nil {
			mod.log.Error("error clearing invalid active contract: %v", clearErr)
		}
		return
	}

	mod.log.Info("hello, %v!", signed.Issuer)

	mod.recordOwnership()

	// Authorization resolves contracts through the auth index, not the config
	// tree; an active-but-unindexed contract would break every delegation chain
	// that terminates at the user.
	err = mod.Auth.IndexContract(mod.ctx, signed)
	if err != nil {
		mod.log.Error("error indexing active contract: %v", err)
	}

	// why: SwarmState holds every membership; a joiner's waits there until
	// its carrier's chain arrives, and the store may open after this runs.
	if err = mod.Nodes.AddSwarmEntry(mod.ctx, signed); err != nil && !errors.Is(err, nodesmod.ErrSwarmEntryHeld) {
		mod.log.Logv(1, "active contract: swarm state: %v", err)
	}

	mod.Nearby.Broadcast()
	mod.runSiblingLinker()
}

// ActiveNodes returns all nodes with an active swarm-membership contract from the given
// user, excluding expelled subjects.
func (mod *Module) ActiveNodes(userID *astral.Identity) (nodes []*astral.Identity) {
	nodes, _ = mod.activeNodes(userID)
	return
}

// activeNodes is ActiveNodes with the error of the ban store read. The
// members are the identities whose membership holds in SwarmState, when
// userID is this node's user; another user has no members here.
//
// why: an unreadable ban store returns no nodes and the error, and not the
// unfiltered list, so an expelled node never reads as a member.
func (mod *Module) activeNodes(userID *astral.Identity) (nodes []*astral.Identity, err error) {
	if userID.IsZero() || !userID.IsEqual(mod.Identity()) {
		return nil, nil
	}

	expelled, err := mod.readExpelledSet(userID)
	if err != nil {
		mod.log.Error("error reading expulsions: %v", err)
		return nil, err
	}

	for _, id := range mod.swarmMembers() {
		if _, banned := expelled[id.String()]; banned {
			continue
		}
		nodes = append(nodes, id)
	}

	return
}

// LocalSwarm returns a list of node identities with an active swarm-membership contract with the current user
func (mod *Module) LocalSwarm() (list []*astral.Identity) {
	list, _ = mod.localSwarm()
	return
}

// localSwarm is LocalSwarm with the error of the contract query. Without an
// active contract the swarm is empty and the error is nil.
func (mod *Module) localSwarm() ([]*astral.Identity, error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil, nil
	}

	return mod.activeNodes(mod.Identity())
}

// IsSwarmMember reports whether id is a current member of the local swarm.
//
// why: a fault in the contract lookup or the ban store read reports no
// member (fail closed); a lookup fault must not read as membership.
//
// note: each call reads the auth index; BenchmarkIsSwarmMember measures it.
func (mod *Module) IsSwarmMember(id *astral.Identity) bool {
	if id.IsZero() {
		return false
	}
	swarm, err := mod.localSwarm()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(swarm, id.IsEqual)
}

// SwarmPeers returns the members of the local swarm other than this node.
func (mod *Module) SwarmPeers() []*astral.Identity {
	return slices.DeleteFunc(mod.LocalSwarm(), mod.node.Identity().IsEqual)
}

// WasOwned reports whether a user contract has been active on this node,
// including one that has since expired or been expelled.
func (mod *Module) WasOwned() bool {
	owned := mod.config.Owned.Get()
	return owned != nil && bool(*owned)
}

// recordOwnership sets the ownership history once.
//
// why at activation and not at claim: every route to an active contract, a
// join and an upgrade of a claimed node included, passes here.
func (mod *Module) recordOwnership() {
	if mod.WasOwned() {
		return
	}
	owned := astral.Bool(true)
	if err := mod.config.Owned.Set(mod.ctx, &owned); err != nil {
		mod.log.Error("recording ownership history: %v", err)
	}
}
