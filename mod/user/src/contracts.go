package user

import (
	"errors"
	"fmt"
	"time"

	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/user"
	userClient "github.com/cryptopunkscc/astrald/mod/user/client"
)

// SetActiveContract sets the contract under which the node operates
func (mod *Module) SetActiveContract(signed *auth.SignedContract) (err error) {
	if err = mod.Auth.VerifyContract(signed); err != nil {
		return
	}

	mod.config.ActiveContract.Set(mod.ctx, signed)

	// synchronize siblings & broadcast
	err = mod.Nearby.Broadcast()
	if err != nil {
		mod.log.Error("error broadcasting presence after setting contract: %v", err)
	}

	mod.runSiblingLinker()
	return
}

// ActiveContract returns the active contract
func (mod *Module) ActiveContract() *auth.SignedContract {
	return mod.activeContract
}

// Identity returns the user identity (the issuer of the active contract), not the local node identity.
func (mod *Module) Identity() *astral.Identity {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil
	}
	return ac.Issuer
}

func (mod *Module) setActiveContract(signed *auth.SignedContract) error {
	switch {
	case signed.IsNil():
		mod.activeContract = nil
		go mod.Nearby.SetMode(mod.ctx, nearby.ModeVisible)
		return nil
	case signed.ExpiresAt.Time().Before(time.Now()):
		return auth.ErrContractExpired
	case !signed.Subject.IsEqual(mod.node.Identity()):
		return errors.New("local node is not the subject of the contract")
	}

	mod.log.Info("hello, %v!", signed.Issuer)
	mod.activeContract = signed
	mod.Nearby.Broadcast()
	return nil
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
