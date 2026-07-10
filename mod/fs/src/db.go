package fs

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func (db *DB) ObjectExists(pathPrefix string, objectID *astral.ObjectID) (b bool, err error) {
	err = db.
		Model(&dbLocalFile{}).
		Where("data_id = ?", objectID).
		Where("path like ?", pathPrefix+"%").
		Where("updated_at != 0").
		Where("deleted_at IS NULL").
		Select("count(*)>0").
		First(&b).Error
	return
}

func (db *DB) FindObject(pathPrefix string, objectID *astral.ObjectID) (rows []*dbLocalFile, err error) {
	err = db.
		Model(&dbLocalFile{}).
		Where("data_id = ?", objectID).
		Where("path like ?", pathPrefix+"%").
		Where("updated_at != 0").
		Where("deleted_at IS NULL").
		Find(&rows).
		Error

	return
}

func (db *DB) UniqueObjectIDs(pathPrefix string) (ids []*astral.ObjectID, err error) {
	err = db.
		Model(&dbLocalFile{}).
		Distinct("data_id").
		Where("path like ?", pathPrefix+"%").
		Where("updated_at != 0").
		Where("deleted_at IS NULL").
		Find(&ids).
		Error

	return
}

func (db *DB) FindByPath(path string) (row *dbLocalFile, err error) {
	err = db.
		Where("path = ?", path).
		Where("deleted_at IS NULL").
		First(&row).
		Error

	return
}

func (db *DB) FindByObjectID(objectID *astral.ObjectID) (rows []*dbLocalFile, err error) {
	err = db.
		Model(&dbLocalFile{}).
		Where("data_id = ?", objectID).
		Where("updated_at != 0").
		Where("deleted_at IS NULL").
		Find(&rows).
		Error

	return
}

// SoftDeletePaths marks paths as deleted
func (db *DB) SoftDeletePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	return db.Model(&dbLocalFile{}).
		Where("path IN ?", paths).
		Update("deleted_at", time.Now()).Error
}

// EachPath calls fn for each path, using primary key pagination.
// If prefix is non-empty, only paths strictly under the prefix are matched (prefix+"/%"),
// not the prefix itself. This is correct for directory roots since only regular files
// are indexed, not directories.
// important: also calls for paths that are invalid (updated_at = 0)
func (db *DB) EachPath(prefix string, fn func(string) error) error {
	const batchSize = 1000
	var lastID int64

	for {
		var rows []dbLocalFile

		query := db.
			Select("id, path").
			Where("id > ?", lastID).
			Where("deleted_at IS NULL").
			Order("id ASC").
			Limit(batchSize)
		if prefix != "" {
			query = query.Where("path LIKE ?", prefix+"/%")
		}

		if err := query.Find(&rows).Error; err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		for _, row := range rows {
			if err := fn(row.Path); err != nil {
				return err
			}
			lastID = row.ID
		}
	}
}

// InvalidatePaths upserts paths with updated_at=0 (needs check).
// If path already exists, resets deleted_at to NULL and updated_at to 0.
func (db *DB) InvalidatePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	records := make([]dbLocalFile, len(paths))
	for i, path := range paths {
		records[i] = dbLocalFile{Path: path}
	}

	return db.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"deleted_at": nil,
				"updated_at": 0,
			}),
		}).
		Create(&records).
		Error
}

// IndexPath inserts/updates index entry for path with given objectID and modTime (does not clear deleted_at)
func (db *DB) IndexPath(
	path string,
	objectID *astral.ObjectID,
	modTime int64,
) error {
	updated := &dbLocalFile{
		Path:      path,
		DataID:    objectID,
		ModTime:   modTime,
		UpdatedAt: time.Now().UnixNano(),
		DeletedAt: nil,
	}

	return db.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"data_id",
				"mod_time",
				"updated_at",
			}),
		}).
		Create(updated).
		Error
}

// ValidatePath marks a path as valid (updates updated_at, clears deleted_at)
func (db *DB) ValidatePath(path string) error {
	return db.Model(&dbLocalFile{}).
		Where("path = ?", path).
		Updates(map[string]interface{}{
			"deleted_at": nil,
			"updated_at": time.Now().UnixNano(),
		}).Error
}

// InvalidateAllPaths marks all paths as invalid
func (db *DB) InvalidateAllPaths() error {
	return db.Model(&dbLocalFile{}).
		Where("id > 0").
		UpdateColumn("updated_at", 0).
		Error
}

// InvalidatePath marks a path for re-check
func (db *DB) InvalidatePath(path string) (err error) {
	return db.Model(&dbLocalFile{}).
		Where("path = ?", path).
		Update("updated_at", 0).Error
}

// EachInvalidatedPath calls fn for each invalidated path (does not include soft deleted paths)
func (db *DB) EachInvalidatedPath(fn func(string) error) error {
	const batchSize = 1000
	var lastID int64

	for {
		var rows []dbLocalFile

		err := db.
			Select("id, path").
			Where("updated_at = 0").
			Where("deleted_at IS NULL").
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(batchSize).
			Find(&rows).
			Error
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		for _, row := range rows {
			if err := fn(row.Path); err != nil {
				return err
			}
			lastID = row.ID
		}
	}
}

func (db *DB) SearchByPath(query string) (rows []*dbLocalFile, err error) {
	err = db.
		Where("LOWER(path) like ?", "%"+query+"%").
		Where("updated_at != 0").
		Where("deleted_at IS NULL").
		Find(&rows).
		Error

	return
}

// LookupPaths returns a map of path -> dbLocalFile for existing entries.
// Includes entries regardless of updated_at or deleted_at state.
func (db *DB) LookupPaths(paths []string) (result map[string]*dbLocalFile, err error) {
	if len(paths) == 0 {
		return make(map[string]*dbLocalFile), nil
	}

	var rows []*dbLocalFile
	err = db.
		Where("path IN ?", paths).
		Find(&rows).
		Error
	if err != nil {
		return result, err
	}

	result = make(map[string]*dbLocalFile, len(rows))
	for _, row := range rows {
		result[row.Path] = row
	}

	return result, nil
}

// ValidatePaths marks paths as valid (sets updated_at=now, clears deleted_at)
func (db *DB) ValidatePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	return db.Model(&dbLocalFile{}).
		Where("path IN ?", paths).
		Updates(map[string]interface{}{
			"deleted_at": nil,
			"updated_at": time.Now().UnixNano(),
		}).Error
}

func (db *DB) DeletePath(path string) (err error) {
	return db.
		Where("path = ?", path).
		Delete(&dbLocalFile{}).
		Error
}
