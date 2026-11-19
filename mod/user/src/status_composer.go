package user

import (
	"math"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

var _ nearbymod.Composer = &Module{}

// ComposeStatus attaches the node's identity to a nearby status by the mode.
// Visible: a PublicProfile of the node, its user and its alias, after an
// adoptable flag when the node has no user. Stealth: a StealthHint of the node
// and its user, or nothing without a user. Silent: nothing. Off the default
// listen ports, the identity is followed by the ports: ListenPorts in Visible,
// SealedPorts in Stealth.
// Spec: .ai/system/protocols/nearby/README.md
func (mod *Module) ComposeStatus(c nearbymod.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeVisible:
		mod.composeVisible(c)
	case nearby.ModeStealth:
		mod.composeStealth(c)
	}
}

// composeVisible attaches the PublicProfile, after an adoptable flag when the node has no user.
func (mod *Module) composeVisible(c nearbymod.Composition) {
	userID := mod.Identity()
	if userID == nil {
		mod.attach(c, nearby.NewFlag("adoptable"))
	}

	mod.attach(c, &nearby.PublicProfile{
		NodeID:    mod.node.Identity(),
		UserID:    userID,
		NodeAlias: mod.profileAlias(),
	})

	if ports := mod.listenPorts(); ports != nil {
		mod.attach(c, ports)
	}
}

// composeStealth attaches the StealthHint of the node and its user, then the
// node's listen ports sealed under the hint's nonce.
//
// note: nearby reads Visible on a node without a user, so a node reaches here
// without one only when its contract ends while the status is composed, and
// then it attaches nothing.
func (mod *Module) composeStealth(c nearbymod.Composition) {
	userID := mod.Identity()
	if userID == nil {
		return
	}

	hint, err := nearby.NewStealthHint(mod.node.Identity(), userID, astral.NewNonce())
	if err != nil {
		mod.log.Error("composing the stealth hint: %v", err)
		return
	}
	mod.attach(c, hint)

	ports := mod.listenPorts()
	if ports == nil {
		return
	}
	sealed, err := nearby.SealPorts(mod.node.Identity(), hint.Nonce, ports)
	if err != nil {
		mod.log.Error("sealing the listen ports: %v", err)
		return
	}
	mod.attach(c, sealed)
}

// listenPorts returns the node's tcp and kcp listen ports, or nil when both are
// the defaults. A missing transport, or a port out of range, reads 0.
func (mod *Module) listenPorts() *nearby.ListenPorts {
	ports := &nearby.ListenPorts{TCP: listenPortOf(mod.Transports.TCP), KCP: listenPortOf(mod.Transports.KCP)}
	if ports.IsDefault() {
		return nil
	}
	return ports
}

// listenPortOf returns the listen port of t, or 0 when t is nil or its port does
// not fit a uint16.
func listenPortOf(t listenPorter) astral.Uint16 {
	if t == nil {
		return 0
	}
	port := t.ListenPort()
	if port < 1 || port > math.MaxUint16 {
		return 0
	}
	return astral.Uint16(port)
}

// profileAlias returns the node's alias, or an empty alias when the node has
// none or when it is longer than a string8 holds.
func (mod *Module) profileAlias() astral.String8 {
	alias, err := mod.Dir.GetAlias(mod.node.Identity())
	if err != nil || len(alias) > math.MaxUint8 {
		return ""
	}
	return astral.String8(alias)
}

// attach adds obj to the composition and logs a refusal.
func (mod *Module) attach(c nearbymod.Composition, obj astral.Object) {
	if err := c.Attach(obj); err != nil {
		mod.log.Errorv(1, "attaching %v to the nearby status: %v", obj.ObjectType(), err)
	}
}
