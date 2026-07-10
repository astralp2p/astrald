package user

import (
	"github.com/astralp2p/astrald/core"
)

var _ core.QueryPreprocessor = &Module{}

// PreprocessQuery attaches routing context to outgoing queries based on the active contract.
// Attaches the active contract to any query whose caller is the issuer.
// Adds relay nodes: all linked siblings when the target is the issuer; all active nodes otherwise.
func (mod *Module) PreprocessQuery(qm *core.QueryModifier) error {
	// get the active contract
	ac := mod.ActiveContract()
	if ac == nil {
		return nil
	}

	// if the query is coming from the active user, attach the active contract
	if qm.Query().Caller.IsEqual(ac.Issuer) {
		qm.Attach(ac)
	}

	if qm.Query().Target.IsEqual(ac.Issuer) {
		// if the target is the active user, attach all siblings
		for _, sib := range mod.getSiblings() {
			qm.AddRelay(sib)
		}
	} else {
		// ... otherwise attach all active nodes
		for _, relay := range mod.ActiveNodes(qm.Query().Target) {
			qm.AddRelay(relay)
		}
	}

	return nil
}
