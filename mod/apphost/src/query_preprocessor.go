package apphost

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

var _ core.QueryPreprocessor = &Module{}

// PreprocessQuery enriches outgoing queries with relay information.
// For queries from a locally-hosted app it attaches a relay-for contract so the
// remote node can verify the app's identity. For queries targeting an app it
// discovers the app's hosting nodes via relay-for contracts and registers them as
// relay candidates on the modifier.
// note: uses a local-only context (ZoneNetwork excluded) to avoid network calls during preprocessing.
func (mod *Module) PreprocessQuery(qm *core.QueryModifier) error {
	ctx := astral.NewContext(nil).WithIdentity(mod.node.Identity()).ExcludeZone(astral.ZoneNetwork)

	// if the query comes from a locally hosted app, attach a contract with the app
	contracts, _ := mod.Auth.SignedContracts().
		WithIssuer(qm.Query().Caller).
		WithSubject(mod.node.Identity()).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx)
	if len(contracts) > 0 {
		qm.Attach(contracts[0])
	}

	// if the query targets an app, find its hosts
	contracts, _ = mod.Auth.SignedContracts().
		WithIssuer(qm.Query().Target).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx)

	for _, contract := range contracts {
		if contract.Subject.IsEqual(mod.node.Identity()) {
			continue
		}
		qm.AddRelay(contract.Subject)
	}

	return nil
}
