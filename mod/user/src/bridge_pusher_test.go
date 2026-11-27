package user

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// bridgePusher is the objects module of a node that reaches one peer only: a
// push to peer is delivered to the peer's user module as a drop from sender,
// and a push to any other node fails as unreachable.
type bridgePusher struct {
	objectsmod.Module
	sender *astral.Identity
	peer   *astral.Identity
	to     *Module
}

func (p *bridgePusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	if !target.IsEqual(p.peer) {
		return errors.New("unreachable")
	}
	return p.to.ReceiveObject(&recordingDrop{sender: p.sender, object: obj})
}
