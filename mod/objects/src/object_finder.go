package objects

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

var _ objects.Finder = &Module{}

// FindObject names this node when its read-default repository holds the object.
// Serves only the device and virtual zones; returns ErrZoneExcluded otherwise.
// The channel is already closed on return.
func (mod *Module) FindObject(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *astral.Identity, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) && !ctx.Zone().Is(astral.ZoneVirtual) {
		return nil, astral.ErrZoneExcluded
	}

	found, err := mod.ReadDefault().Contains(ctx, objectID)
	if err != nil {
		return nil, err
	}

	out := make(chan *astral.Identity, 1)
	defer close(out)

	if found {
		out <- mod.node.Identity()
	}

	return out, nil
}
