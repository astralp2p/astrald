package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nearby"
)

var _ nearby.Composer = &Module{}

// ComposeStatus attaches identity claims to a nearby composition based on the active mode.
// In Visible mode, attaches the active contract or an adoptable flag with a public profile.
// In Stealth mode, attaches a StealthHint only when an active contract exists; emits nothing otherwise.
// In Silent mode, does nothing.
func (mod *Module) ComposeStatus(a nearby.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeSilent:
		// no-op
	case nearby.ModeVisible:
		if c := mod.ActiveContract(); c != nil {
			a.Attach(c)
		} else {
			alias, _ := mod.Dir.GetAlias(mod.node.Identity())
			a.Attach(nearby.NewFlag("adoptable"))
			a.Attach(&nearby.PublicProfile{
				NodeID:    mod.node.Identity(),
				NodeAlias: alias,
			})
		}

	case nearby.ModeStealth:
		ac := mod.ActiveContract()
		if ac == nil {
			return
		}
		nonce := astral.NewNonce()
		a.Attach(&nearby.StealthHint{
			Commitment: nearby.ComputeCommitment(ac.Issuer, nonce),
			MaskedID:   nearby.MaskIdentity(mod.node.Identity(), ac.Issuer),
			Nonce:      nonce,
		})
	}
}
