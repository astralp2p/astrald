package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

func (mod *Module) AddHolder(h objects.Holder) error {
	return mod.holders.Add(h)
}

func (mod *Module) Holders(objectID *astral.ObjectID) (holders []objects.Holder) {
	for _, h := range mod.holders.Clone() {
		if h.HoldObject(objectID) {
			holders = append(holders, h)
		}
	}
	return
}
