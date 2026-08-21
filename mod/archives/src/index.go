package archives

import (
	"context"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/archives"
)

type entryFunc func(*archives.Entry)

// Index scans and persists the archive for objectID, returning a cached result
// if one already exists. Emits EventArchiveIndexed on success.
// note: serialized under mod.mu to prevent concurrent scans of the same object.
func (mod *Module) Index(ctx context.Context, objectID *astral.ObjectID) (archive *archives.Archive, err error) {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if cached := mod.getCache(objectID); cached != nil {
		return cached, nil
	}

	mod.log.Logv(1, "indexing zip %v", objectID)
	archive, err = mod.scan(ctx, objectID, func(entry *archives.Entry) {
		mod.log.Infov(1, "scanned %v (%v)", entry.ObjectID, entry.Path)
	})
	if err != nil {
		return
	}

	err = mod.setCache(objectID, archive)

	mod.Objects.Receive(&archives.EventArchiveIndexed{ObjectID: objectID, Archive: archive}, nil)

	return
}

func (mod *Module) scan(ctx context.Context, objectID *astral.ObjectID, postScan entryFunc) (archive *archives.Archive, err error) {
	reader, err := mod.openZip(objectID)
	if err != nil {
		return nil, fmt.Errorf("error reading zip file: %w", err)
	}

	archive = &archives.Archive{
		Comment: reader.Comment,
		Format:  "zip",
	}

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		f, err := file.Open()
		if err != nil {
			mod.log.Errorv(1, "open %v: %v", file.Name, err)
			continue
		}

		fileID, err := astral.Resolve(f)
		f.Close()
		if err != nil {
			mod.log.Errorv(1, "resolve %v: %v", file.Name, err)
			continue
		}

		entry := &archives.Entry{
			ObjectID: fileID,
			Path:     file.Name,
			Comment:  file.Comment,
			Modified: file.Modified,
		}

		archive.Entries = append(archive.Entries, entry)

		if postScan != nil {
			postScan(entry)
		}

		select {
		case <-ctx.Done():
			return archive, ctx.Err()
		default:
		}
	}

	return
}

func (mod *Module) Forget(objectID *astral.ObjectID) error {
	return mod.clearCache(objectID)
}

func (mod *Module) getCache(objectID *astral.ObjectID) (archive *archives.Archive) {
	var row dbArchive

	err := mod.db.
		Where("object_id = ?", objectID).
		Preload("Entries").
		First(&row).
		Error
	if err != nil {
		return
	}

	archive = &archives.Archive{
		Comment: row.Comment,
		Format:  row.Format,
	}

	for _, e := range row.Entries {
		archive.Entries = append(archive.Entries, &archives.Entry{
			ObjectID: e.ObjectID,
			Path:     e.Path,
			Comment:  e.Comment,
			Modified: e.Modified,
		})
	}

	return
}

func (mod *Module) clearCache(objectID *astral.ObjectID) (err error) {
	var id int
	err = mod.db.
		Model(&dbArchive{}).
		Select("id").
		Where("object_id = ?", objectID).
		First(&id).
		Error
	if err != nil {
		return
	}
	err = mod.db.
		Where("parent_id = ?", id).
		Delete(&dbEntry{}).
		Error
	if err != nil {
		return
	}
	return mod.db.
		Where("object_id = ?", objectID).
		Delete(&dbArchive{}).
		Error
}

func (mod *Module) setCache(objectID *astral.ObjectID, archive *archives.Archive) error {
	mod.clearCache(objectID)

	row := dbArchive{
		ObjectID: objectID,
		Comment:  archive.Comment,
		Format:   archive.Format,
	}

	for _, entry := range archive.Entries {
		row.Entries = append(row.Entries, dbEntry{
			ObjectID: entry.ObjectID,
			Path:     entry.Path,
			Comment:  entry.Comment,
			Modified: entry.Modified,
		})
	}

	return mod.db.Create(&row).Error
}
