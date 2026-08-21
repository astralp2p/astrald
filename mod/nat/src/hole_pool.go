package nat

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/nat"
)

// HolePool is a concurrent-safe registry of active NAT holes, keyed by nonce.
type HolePool struct {
	*Module
	holes sig.Map[astral.Nonce, *Hole]
}

func NewHolePool(mod *Module) *HolePool {
	return &HolePool{
		Module: mod,
		holes:  sig.Map[astral.Nonce, *Hole]{},
	}
}

// Add registers a hole in the pool; returns ErrDuplicateHole if a hole with the same nonce already exists.
func (p *HolePool) Add(hole *Hole) error {
	_, ok := p.holes.Set(hole.Nonce, hole)
	if !ok {
		return nat.ErrDuplicateHole
	}
	return nil
}

func (p *HolePool) Get(nonce astral.Nonce) (*Hole, bool) {
	return p.holes.Get(nonce)
}

func (p *HolePool) GetAll() []*Hole {
	return p.holes.Values()
}

// TakeAny removes and returns the first hole that matches the given peer identity.
func (p *HolePool) TakeAny(peer *astral.Identity) (*Hole, error) {
	for _, hole := range p.holes.Values() {
		if hole.MatchesPeer(peer) {
			p.Remove(hole.Nonce)
			return hole, nil
		}
	}
	return nil, nat.ErrHoleNotExists
}

func (p *HolePool) Take(nonce astral.Nonce) (*Hole, error) {
	hole, ok := p.holes.Delete(nonce)
	if !ok {
		return nil, nat.ErrHoleNotExists
	}
	return hole, nil
}

func (p *HolePool) Remove(nonce astral.Nonce) (hole *Hole, ok bool) {
	return p.holes.Delete(nonce)
}
