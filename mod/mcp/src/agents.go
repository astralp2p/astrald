package mcp

import (
	"errors"

	authapi "github.com/astralp2p/astral-go/api/auth"
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

	signed := &authapi.SignedContract{Contract: contract}

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
// tenants' agents a generated name contends in a namespace none of them owns,
// and one tenant loses it silently. A caller that wants an alias passes one; a
// caller that does not — the dashboard, which carries its own display name —
// leaves the agent without.
func (mod *Module) assignAlias(agentID *astral.Identity, alias string) (string, error) {
	if alias == "" {
		return "", nil
	}

	if _, err := mod.Dir.ResolveIdentity(alias); err == nil {
		return "", errors.New("alias already taken")
	}

	return alias, mod.Dir.SetAlias(agentID, alias)
}

// deleteAgent revokes the agent's token, unsets its alias and removes its row.
// The signed relay contract stays indexed until it expires.
func (mod *Module) deleteAgent(row *dbAgent) error {
	err := mod.Apphost.DeleteAccessToken(row.Token)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if row.Alias != "" {
		if err = mod.Dir.SetAlias(row.Identity, ""); err != nil {
			return err
		}
	}

	_ = mod.agentIDs.Remove(row.Identity.String())
	_ = mod.exposed.Remove(row.Identity.String())
	mod.drainListener(row.Identity)
	mod.dropPending(row.Identity)
	mod.closeAgentSessions(row.Identity)

	return mod.db.DeleteAgent(row.Identity)
}
