package mcp

import (
	"errors"

	authapi "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/lib/aliasgen"
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

// assignAlias binds alias to the agent, generating a free random one when
// alias is empty. Returns the alias actually set.
func (mod *Module) assignAlias(agentID *astral.Identity, alias string) (string, error) {
	if alias != "" {
		if _, err := mod.Dir.ResolveIdentity(alias); err == nil {
			return "", errors.New("alias already taken")
		}
		return alias, mod.Dir.SetAlias(agentID, alias)
	}

	// why: aliasgen's namespace is small; probe for a free name a few times.
	for i := 0; i < 5; i++ {
		candidate := aliasgen.New()
		if _, err := mod.Dir.ResolveIdentity(candidate); err == nil {
			continue
		}
		return candidate, mod.Dir.SetAlias(agentID, candidate)
	}

	return "", errors.New("alias generation exhausted")
}

// deleteAgent revokes the agent's token, unsets its alias and removes its row.
// The signed relay contract stays indexed until it expires.
func (mod *Module) deleteAgent(row *dbAgent) error {
	err := mod.Apphost.DeleteAccessToken(row.Token)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if err = mod.Dir.SetAlias(row.Identity, ""); err != nil {
		return err
	}

	_ = mod.agentIDs.Remove(row.Identity.String())
	mod.drainListener(row.Identity)
	mod.dropPending(row.Identity)
	mod.closeAgentSessions(row.Identity)

	return mod.db.DeleteAgent(row.Identity)
}
