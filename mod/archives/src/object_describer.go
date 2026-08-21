package archives

import (
	"errors"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/archives"
)

var _ objects.Describer = &Module{}

// DescribeObject returns a descriptor for a previously-indexed archive.
// Only serves ZoneDevice; returns ErrZoneExcluded for all other zones.
func (mod *Module) DescribeObject(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	return mod.describeArchive(objectID)
}

func (mod *Module) describeArchive(objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	var archive = mod.getCache(objectID)
	if archive == nil {
		return nil, errors.New("description unavailable")
	}

	var totalSize uint64
	for _, e := range archive.Entries {
		totalSize += e.ObjectID.Size
	}

	var results = make(chan *objects.Descriptor, 1)
	defer close(results)

	results <- &objects.Descriptor{
		SourceID: mod.node.Identity(),
		ObjectID: objectID,
		Data: &archives.ArchiveDescriptor{
			Format:    astral.String8(archive.Format),
			Entries:   astral.Uint32(len(archive.Entries)),
			TotalSize: astral.Uint64(totalSize),
		},
	}

	return results, nil
}
