package archives

import (
	_zip "archive/zip"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"io"
)

// OpenObject serves an archive entry as a readable stream by locating it
// across all known parent archives. Only serves ZoneVirtual.
func (mod *Module) OpenObject(ctx *astral.Context, objectID *astral.ObjectID) (io.ReadCloser, error) {
	if !ctx.Zone().Is(astral.ZoneVirtual) {
		return nil, astral.ErrZoneExcluded
	}

	if objects.IsOffsetLimitValid(objectID, 0, 0) {
		return nil, objects.ErrOutOfBounds
	}

	var rows []dbEntry
	err := mod.db.
		Unscoped().
		Preload("Parent").
		Where("object_id = ?", objectID).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		r, err := mod.open(row.Parent.ObjectID, row.Path, row.ObjectID)
		if err == nil {
			mod.log.Logv(2, "opened %v from %v/%v", objectID, row.Parent.ObjectID, row.Path)
			return r, nil
		}
	}

	return nil, objects.ErrNotFound
}

func (mod *Module) open(zipID *astral.ObjectID, path string, fileID *astral.ObjectID) (io.ReadCloser, error) {
	zipFile, err := mod.openZip(zipID)
	if err != nil {
		return nil, objects.ErrNotFound
	}

	var r = &contentReader{
		zip:      zipFile,
		path:     path,
		objectID: fileID,
	}

	err = r.open()

	return r, err
}

func (mod *Module) openZip(objectID *astral.ObjectID) (*_zip.Reader, error) {
	var r = &readerAt{
		identity: mod.node.Identity(),
		objects:  mod.Objects,
		objectID: objectID,
	}

	zipFile, err := _zip.NewReader(r, int64(objectID.Size))
	return zipFile, err
}
