package fs

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/fs"
)

var _ objects.Describer = &Module{}

// DescribeObject returns file-location descriptors for objectID; restricted to ZoneDevice callers.
func (mod *Module) DescribeObject(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	rows, err := mod.db.FindByObjectID(objectID)
	if err != nil {
		return nil, err
	}

	var results = make(chan *objects.Descriptor, len(rows))
	defer close(results)

	for _, row := range rows {
		results <- &objects.Descriptor{
			SourceID: mod.node.Identity(),
			ObjectID: objectID,
			Data: &fs.FileLocation{
				NodeID: mod.node.Identity(),
				Path:   astral.String16(row.Path),
			},
		}
	}

	return results, nil
}
