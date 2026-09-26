package auth

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// claimExpirySlack is how far past the default validity of
// user.new_node_contract a claim contract may expire.
//
// why: a client may build the contract with a clock ahead of the node's.
const claimExpirySlack = time.Hour

// authorizeContract reports whether caller may obtain c signed as both of its
// parties.
//
// why the crypto signer rule and not one of its own: a contract signature is a
// signature under the party's key, and crypto.sign_hash already yields one
// over Contract.SignableHash. A stricter rule is bypassed there; a looser one
// is a second way to spend a key the caller does not hold.
//
// why the claim exception: node setup asks for the user→node contract as the
// node, a caller that is neither the user nor holds sudo for it, and whose own
// key the signer rule refuses on the self branch. isNodeClaim admits that one
// contract and nothing else.
func (mod *Module) authorizeContract(ctx *astral.Context, caller *astral.Identity, c *auth.Contract) error {
	if c == nil || c.Issuer.IsZero() || c.Subject.IsZero() {
		return auth.ErrInvalidContract
	}

	err := mod.authorizeParties(ctx, caller, c)
	if err != nil && mod.isNodeClaim(ctx, caller, c) {
		return nil
	}
	return err
}

// authorizeParties applies the crypto signer rule to the issuer, then the
// subject.
//
// why "authorize" and not "sign as": setup clients match "sign as issuer:" to
// tell a missing key from other failures, so a refusal reads apart from it.
func (mod *Module) authorizeParties(ctx *astral.Context, caller *astral.Identity, c *auth.Contract) error {
	if err := mod.Crypto.AuthorizeSigner(ctx, caller, secp256k1.FromIdentity(c.Issuer)); err != nil {
		return fmt.Errorf("authorize issuer: %w", err)
	}
	if err := mod.Crypto.AuthorizeSigner(ctx, caller, secp256k1.FromIdentity(c.Subject)); err != nil {
		return fmt.Errorf("authorize subject: %w", err)
	}
	return nil
}

// isNodeClaim reports whether c is the contract that claims this unclaimed
// node, asked for by the node's own session: issued by another identity to
// this node, carrying exactly the management node permits, expiring within
// the validity setup gives it, from an issuer that never issued this node a
// relay contract or a node contract.
//
// why the caller is the node: every setup caller arrives as the node, an
// anonymous session rewritten by core.Router or the node's own token.
//
// why the issuer is not the node: a node claimed by itself makes its own
// caller the user.
//
// why the cheap tests run first: isUnclaimed may wait and issuedToNode reads
// the database, and most refusals end before either.
func (mod *Module) isNodeClaim(ctx *astral.Context, caller *astral.Identity, c *auth.Contract) bool {
	node := mod.node.Identity()
	return caller.IsEqual(node) &&
		c.Subject.IsEqual(node) &&
		!c.Issuer.IsEqual(node) &&
		isManagementNodeContract(c) &&
		hasClaimExpiry(c, time.Now()) &&
		mod.isUnclaimed(ctx) &&
		!mod.issuedToNode(c.Issuer)
}

// isManagementNodeContract reports whether c carries exactly the permits
// user.new_node_contract writes for a management node: same order, action and
// delegation, and no constraint.
//
// why exact: any extra or wider permit, such as mod.auth.sudo_action, would be
// signed in the issuer's name without the issuer's consent.
func isManagementNodeContract(c *auth.Contract) bool {
	want, err := user.NewNodeContract(c.Issuer, c.Subject, true, 0)
	if err != nil || len(c.Permits) != len(want.Permits) {
		return false
	}
	for i, p := range c.Permits {
		w := want.Permits[i]
		if p == nil || p.Action != w.Action || p.Delegation != w.Delegation {
			return false
		}
		if p.Constraints != nil && len(p.Constraints.Objects()) > 0 {
			return false
		}
	}
	return true
}

// hasClaimExpiry reports whether c expires later than the shortest validity
// user.accept_membership accepts, and no later than the default validity of
// user.new_node_contract plus claimExpirySlack.
//
// why the ceiling: the exception signs in the issuer's name without the
// issuer's consent, so a claim signed through a misuse of it lapses no later
// than setup's own claim would.
//
// why the floor: user.accept_membership refuses a node contract that expires
// sooner, and the claim is held to the same bound.
func hasClaimExpiry(c *auth.Contract, now time.Time) bool {
	expiresAt := c.ExpiresAt.Time()
	return expiresAt.After(now.Add(usermod.MinimalContractLength)) &&
		!expiresAt.After(now.Add(usermod.DefaultContractValidity+claimExpirySlack))
}

// isUnclaimed reports whether the node has no user. It waits for the user
// module to apply its stored contract, and answers false without the module.
//
// why wait for Ready: Run clears an invalid stored contract before Ready
// closes, so the answer after Ready is the settled one, as apphost's
// inSetupMode reads it.
func (mod *Module) isUnclaimed(ctx *astral.Context) bool {
	if mod.User == nil {
		return false
	}
	select {
	case <-mod.User.Ready():
		return mod.User.Identity().IsZero()
	case <-ctx.Done():
		return false
	}
}

// issuedToNode reports whether the index holds a contract from issuer to this
// node carrying mod.nodes.relay_for_action or mod.user.swarm_membership_action,
// expired ones included. A failed lookup answers true, so the exception closes
// on error.
//
// why relay contracts: apphost.register and mcp.create_agent mint each app and
// agent identity with a relay contract to the node, so the exception never
// signs as a key the node minted. The setup user's key is stored, not minted,
// and carries none.
//
// why node contracts: the user module indexes every node contract it
// activates, so the exception never signs again for a user this node has had,
// whether the tree value naming that user was cleared or its contract lapsed.
//
// why expired ones: a contract lapses, and the key it names stays on the node.
func (mod *Module) issuedToNode(issuer *astral.Identity) bool {
	found, err := mod.db.contractIssued(issuer, mod.node.Identity(), []string{
		nodes.RelayForAction{}.ObjectType(),
		user.SwarmMembershipAction{}.ObjectType(),
	})
	return err != nil || found
}
