package mcp

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/apphost"
	"gorm.io/gorm"
)

// createAgentIdentity mints a fresh agent identity: a stored + indexed key and
// a signed node relay contract, mirroring apphost's register flow.
func (mod *Module) createAgentIdentity(ctx *astral.Context) (*astral.Identity, error) {
	key := secp256k1.New()

	if _, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), key); err != nil {
		return nil, err
	}

	if err := mod.Crypto.AddToIndex(key); err != nil {
		return nil, err
	}

	agentID := secp256k1.Identity(secp256k1.PublicKey(key))

	contract, err := apphost.NewAppContract(agentID, mod.node.Identity(), AgentContractDuration)
	if err != nil {
		return nil, err
	}

	signed := &auth.SignedContract{Contract: contract}

	if err = mod.Auth.SignContract(ctx, signed); err != nil {
		return nil, err
	}

	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return nil, err
	}

	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return nil, err
	}

	return agentID, nil
}

// assignAlias binds alias to the agent when one is given, and binds nothing
// when it is empty. Returns the alias actually set.
//
// why nothing is generated: an alias is node-global, so on a node holding many
// tenants' agents a generated name contends in a namespace none of them owns.
func (mod *Module) assignAlias(agentID *astral.Identity, alias string) (string, error) {
	if alias == "" {
		return "", nil
	}

	if _, err := mod.Dir.ResolveIdentity(alias); err == nil {
		return "", errors.New("alias already taken")
	}

	return alias, mod.Dir.SetAlias(agentID, alias)
}

// registerAgent stores the agent row and mirrors its identity into the set
// RouteQuery reads.
//
// why the store first: a mirror ahead of a failed write would route on a
// decision nothing recorded.
func (mod *Module) registerAgent(row *dbAgent) error {
	if err := mod.db.CreateAgent(row); err != nil {
		return err
	}

	_ = mod.agentIDs.Add(row.Identity.String())

	return nil
}

// deleteAgent revokes the agent's token, withdraws every grant its identity
// holds, unsets its alias and removes its row, taking the mail it owns with it
// — both boxes, archived or not. A correspondent's own copy of the same message
// is owned by the correspondent and stays. The signed relay contract stays
// indexed until it expires.
func (mod *Module) deleteAgent(row *dbAgent) error {
	err := mod.Apphost.DeleteAccessToken(row.Token)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if err = mod.revokeGrants(row.Identity); err != nil {
		return err
	}

	if row.Alias != "" {
		if err = mod.Dir.SetAlias(row.Identity, ""); err != nil {
			return err
		}
	}

	_ = mod.agentIDs.Remove(row.Identity.String())

	return mod.db.DeleteAgent(row.Identity)
}

// revokeGrants withdraws every node-local grant identity holds. A grant names
// the identity and nothing else, so one left behind keeps authorizing whoever
// presents the deleted agent's identity.
//
// why every permit and not the unexpired ones: Grants reports expired rows too,
// and a row outlives the agent that held it. Deleting an expired grant takes
// away nothing the node was still honoring.
//
// why a failure stops the deletion: the agent row is the only record naming the
// identity, so a run that removed the row and left a grant standing would leave
// authority mcp.delete_agent can no longer reach. The kept row makes the call
// repeatable.
func (mod *Module) revokeGrants(identity *astral.Identity) error {
	permits, err := mod.Apphost.Grants(identity)
	if err != nil {
		return err
	}

	for _, permit := range permits {
		// note: a concurrent revoke takes the row between the listing and here.
		err = mod.Apphost.Revoke(identity, string(permit.Action))
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return nil
}
