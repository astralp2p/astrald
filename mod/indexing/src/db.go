package indexing

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/indexing"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

func newDB(gormDB *gorm.DB) (*DB, error) {
	return &DB{DB: gormDB}, nil
}

// InTx runs fn inside a transaction. The wrapped *DB shares the same method set,
// so any (db *DB) helper called on tx runs against the same transaction.
func (db *DB) InTx(fn func(tx *DB) error) error {
	return db.Transaction(func(txGorm *gorm.DB) error {
		return fn(&DB{DB: txGorm})
	})
}

func (db *DB) autoMigrate() error {
	return db.AutoMigrate(
		&dbRepoEntry{},
	)
}

// latestExistingObjectIDs returns the set of object IDs whose latest change in repoName
// has Exist=true (the current logical state).
func (db *DB) latestExistingObjectIDs(repoName string) ([]*astral.ObjectID, error) {
	var ids []*astral.ObjectID
	err := db.
		Raw(`
			SELECT object_id FROM `+(dbRepoEntry{}).TableName()+` r1
			WHERE r1.repo = ? AND r1.exist = ?
			  AND r1.version = (
			      SELECT MAX(version) FROM `+(dbRepoEntry{}).TableName()+` r2
			      WHERE r2.repo = r1.repo AND r2.object_id = r1.object_id
			  )
		`, repoName, true).
		Scan(&ids).
		Error
	return ids, err
}

// findExcessObjectIDs returns object IDs whose latest change says Exist=true but
// which are not in the given snapshot.
func (db *DB) findExcessObjectIDs(repoName string, snapshot []*astral.ObjectID) ([]*astral.ObjectID, error) {
	existing, err := db.latestExistingObjectIDs(repoName)
	if err != nil {
		return nil, err
	}

	keep := make(map[string]struct{}, len(snapshot))
	for _, id := range snapshot {
		keep[id.String()] = struct{}{}
	}

	var excess []*astral.ObjectID
	for _, id := range existing {
		if _, ok := keep[id.String()]; !ok {
			excess = append(excess, id)
		}
	}
	return excess, nil
}

// findMissingObjectIDs returns object IDs from the snapshot whose latest change
// is missing or Exist=false.
func (db *DB) findMissingObjectIDs(repoName string, snapshot []*astral.ObjectID) ([]*astral.ObjectID, error) {
	existing, err := db.latestExistingObjectIDs(repoName)
	if err != nil {
		return nil, err
	}

	present := make(map[string]struct{}, len(existing))
	for _, id := range existing {
		present[id.String()] = struct{}{}
	}

	var missing []*astral.ObjectID
	for _, id := range snapshot {
		if _, ok := present[id.String()]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// addToRepo records that objectID is now in repoName. Returns
// ErrObjectAlreadyAdded if the latest entry already says Exist=true.
func (db *DB) addToRepo(repoName string, objectID *astral.ObjectID) error {
	return db.InTx(func(tx *DB) error {
		exists, err := tx.latestExists(repoName, objectID)
		if err != nil {
			return err
		}
		if exists {
			return indexing.ErrObjectAlreadyAdded
		}

		var maxVer *uint64
		err = tx.Model(&dbRepoEntry{}).
			Where("repo = ?", repoName).
			Select("MAX(version)").
			Scan(&maxVer).Error
		if err != nil {
			return err
		}
		version := uint64(1)
		if maxVer != nil {
			version = *maxVer + 1
		}

		return tx.Create(&dbRepoEntry{
			Repo: repoName, Version: version, ObjectID: objectID, Exist: true,
		}).Error
	})
}

// removeFromRepo records that objectID has left repoName. Errors if the
// latest entry already says Exist=false.
func (db *DB) removeFromRepo(repoName string, objectID *astral.ObjectID) error {
	return db.InTx(func(tx *DB) error {
		exists, err := tx.latestExists(repoName, objectID)
		if err != nil {
			return err
		}
		if !exists {
			return indexing.ErrObjectNotPresent
		}

		var maxVer *uint64
		err = tx.Model(&dbRepoEntry{}).
			Where("repo = ?", repoName).
			Select("MAX(version)").
			Scan(&maxVer).Error
		if err != nil {
			return err
		}
		version := uint64(1)
		if maxVer != nil {
			version = *maxVer + 1
		}

		return tx.Create(&dbRepoEntry{
			Repo: repoName, Version: version, ObjectID: objectID, Exist: false,
		}).Error
	})
}

// latestExists reports whether the most recent change for (repo, objectID) is Exist=true.
func (db *DB) latestExists(repoName string, objectID *astral.ObjectID) (bool, error) {
	var row dbRepoEntry
	err := db.
		Where("repo = ? AND object_id = ?", repoName, objectID).
		Order("version DESC").
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Exist, nil
}

// nextChange returns the next change in repoName with version > afterVersion (oldest first),
// or (nil, nil) when caught up.
func (db *DB) nextChange(repoName string, afterVersion uint64) (*dbRepoEntry, error) {
	var row dbRepoEntry
	err := db.
		Where("repo = ? AND version > ?", repoName, afterVersion).
		Order("version ASC").
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
