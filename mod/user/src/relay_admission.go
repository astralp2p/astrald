package user

import (
	"errors"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// AdmitRelayContract is the one rule for every path that indexes a relay-for
// contract (astral-docs topics/app-routing.md, Reach). A relay-for contract is
// any contract with a relay-for permit, alone or beside other permits. It
// answers nil for any other contract: those keep their own rules.
//
// A relay-for contract is admitted when its subject is this node or a member of
// the local swarm, both signatures verify, and it has not expired. A non-nil
// sender delivered the contract and must be a member too; a nil sender is a
// local path, such as an app registration or the auth indexer.
//
// why the subject need not be the sender: any member may forward the contracts
// of the others, so a new member learns the swarm's apps from its first link.
// why the self clause: an unclaimed node has no swarm, yet indexes the relay
// contracts of the apps it hosts.
// why a relay-for permit beside others counts: routing finds hosts by the
// relay-for permit alone (apphost PreprocessQuery), so such a contract routes too.
func (mod *Module) AdmitRelayContract(sender *astral.Identity, sc *auth.SignedContract) error {
	if sc.IsNil() || !grantsRelayFor(sc.Contract) {
		return nil
	}

	if sc.Issuer.IsZero() || sc.Subject.IsZero() {
		return errors.New("incomplete contract")
	}

	// note: LocalSwarm leaves out expelled nodes, and is empty when its query fails.
	swarm := mod.LocalSwarm()

	if !sc.Subject.IsEqual(mod.node.Identity()) && !slices.ContainsFunc(swarm, sc.Subject.IsEqual) {
		return errors.New("subject is not a swarm member")
	}

	if sender != nil && !slices.ContainsFunc(swarm, sender.IsEqual) {
		return errors.New("sender is not a swarm member")
	}

	if err := mod.Auth.VerifyContract(sc); err != nil {
		return err
	}

	if sc.ExpiresAt.Time().Before(time.Now()) {
		return auth.ErrContractExpired
	}

	return nil
}

// grantsRelayFor reports whether c carries a relay-for permit.
func grantsRelayFor(c *auth.Contract) bool {
	return c != nil && len(c.HasPermit(nodes.RelayForAction{}.ObjectType())) > 0
}
