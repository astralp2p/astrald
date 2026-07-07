package user

import (
	"errors"
	"fmt"
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/user"
	userClient "github.com/cryptopunkscc/astral-go/api/user/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/nearby"
)

// validateActiveContract enforces the invariant of the active-contract slot:
// both signatures valid, this node is the subject, not expired, and it grants
// swarm membership. Source-independent - the same gate for local setup,
// cold-card, and remote membership.
func (mod *Module) validateActiveContract(signed *auth.SignedContract) error {
	err := mod.Auth.VerifyContract(signed)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if !signed.Subject.IsEqual(mod.node.Identity()) {
		return errors.New("local node is not the subject of the contract")
	}
	if signed.ExpiresAt.Time().Before(time.Now()) {
		return auth.ErrContractExpired
	}
	if !user.IsNodeContract(signed.Contract) {
		return errors.New("contract does not grant swarm membership")
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

// ActiveContract returns the active contract from the tree store - the single
// source of truth. Nil when the node is unclaimed.
func (mod *Module) ActiveContract() *auth.SignedContract {
	return mod.config.ActiveContract.Get()
}

// Identity returns the user identity (the issuer of the active contract), not the local node identity.
func (mod *Module) Identity() *astral.Identity {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil
	}
	return ac.Issuer
}

// onActiveContractChanged reacts to a change of the active-contract store,
// driven by Follow at startup and on every write. It keeps no copy - readers
// use ActiveContract(). A stored value that fails validation (stale, corrupt,
// or a stray write) is scrubbed so it never stays live.
func (mod *Module) onActiveContractChanged(signed *auth.SignedContract) {
	if signed.IsNil() {
		go mod.Nearby.SetMode(mod.ctx, nearby.ModeVisible)
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

	// Authorization resolves contracts through the auth index, not the config
	// tree; an active-but-unindexed contract would break every delegation chain
	// that terminates at the user.
	err = mod.Auth.IndexContract(mod.ctx, signed)
	if err != nil {
		mod.log.Error("error indexing active contract: %v", err)
	}

	mod.Nearby.Broadcast()
	mod.runSiblingLinker()
}

// ActiveNodeContracts returns all active swarm-membership contracts issued by userID,
// excluding contracts whose subject has been expelled.
func (mod *Module) ActiveNodeContracts(userID *astral.Identity) ([]*auth.SignedContract, error) {
	contracts, err := mod.Auth.SignedContracts().WithIssuer(userID).WithAction(&user.SwarmMembershipAction{}).Find(mod.ctx)
	if err != nil {
		return nil, err
	}

	expelled := mod.expelledSet(userID)

	filtered := make([]*auth.SignedContract, 0, len(contracts))
	for _, c := range contracts {
		if _, banned := expelled[c.Subject.String()]; banned {
			continue
		}
		filtered = append(filtered, c)
	}

	return filtered, nil
}

// ActiveNodes returns all nodes with an active swarm-membership contract from the given
// user, excluding expelled subjects.
func (mod *Module) ActiveNodes(userID *astral.Identity) (nodes []*astral.Identity) {
	contracts, err := mod.Auth.
		SignedContracts().
		WithIssuer(userID).
		WithAction(&user.SwarmMembershipAction{}).
		Find(mod.ctx)

	if err != nil {
		mod.log.Error("error getting active nodes: %v", err)
		return
	}

	expelled := mod.expelledSet(userID)

	for _, c := range contracts {
		if _, banned := expelled[c.Subject.String()]; banned {
			continue
		}
		nodes = append(nodes, c.Subject)
	}

	return
}

// LocalSwarm returns a list of node identities with an active swarm-membership contract with the current user
func (mod *Module) LocalSwarm() (list []*astral.Identity) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}

	return mod.ActiveNodes(ac.Issuer)
}

// IssueMembership mints a swarm-membership contract for nodeID, collects the remote node's subject signature, and verifies both before returning.
// Requires an active contract; the user identity becomes the issuer.
// Refuses nodeID with user.ErrExpelled if the issuer has banned it.
func (mod *Module) IssueMembership(ctx *astral.Context, nodeID *astral.Identity) (signed *auth.SignedContract, err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil, user.ErrNoActiveContract
	}

	// why: sole chokepoint for OpAdopt and OpRequestMembership — refusing here
	// blocks re-admission of a banned node before any signing or network handshake,
	// rather than letting the membership filter hide a freshly minted contract.
	if mod.isExpelled(ac.Issuer, nodeID) {
		return nil, user.ErrExpelled
	}

	// adopted and requesting nodes join as plain members without management permits
	contract, err := user.NewNodeContract(ac.Issuer, nodeID, false, defaultContractValidity)
	if err != nil {
		return nil, err
	}

	signed = &auth.SignedContract{Contract: contract}

	issuerSig, err := mod.Auth.SignIssuer(ctx, signed)
	if err != nil {
		return nil, fmt.Errorf("sign as issuer: %w", err)
	}

	subjectSig, err := userClient.New(nodeID, nil).AcceptMembership(ctx, contract, issuerSig)
	if err != nil {
		return nil, err
	}

	signed.IssuerSig = issuerSig
	signed.SubjectSig = subjectSig

	if err = mod.Auth.VerifySubject(signed); err != nil {
		return nil, fmt.Errorf("subject sig verification: %w", err)
	}
	return signed, nil
}
