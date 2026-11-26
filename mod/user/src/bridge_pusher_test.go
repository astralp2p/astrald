package user

import (
	"errors"
	"sync"

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

	mu      sync.Mutex
	refused []*astral.Identity
}

func (p *bridgePusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	if !target.IsEqual(p.peer) {
		p.refuse(target)
		return errors.New("unreachable")
	}
	return p.to.ReceiveObject(&recordingDrop{sender: p.sender, object: obj})
}

func (p *bridgePusher) refuse(target *astral.Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refused = append(p.refused, target)
}

func (p *bridgePusher) unreachable() []*astral.Identity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*astral.Identity(nil), p.refused...)
}
