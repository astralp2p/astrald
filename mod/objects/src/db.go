package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

// Migrate drops the old tracking table and creates the tracking table.
// note: content tracked only by the old table has no row and is never purged; there is no migration (VFS 6, §11 "objects: tracking").
func (db *DB) Migrate() error {
	err := db.Migrator().DropTable(dbObjectsTableName)
	if err != nil {
		return err
	}
	return db.AutoMigrate(&dbTracking{})
}

// trackCommit writes the commit time of objectID in the leaf repository repo, creating the row when it is missing.
func (db *DB) trackCommit(repo string, objectID *astral.ObjectID, committedAt int64) error {
	return db.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository"}, {Name: "object_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"committed_at"}),
	}).Create(&dbTracking{
		Repository:  repo,
		ObjectID:    objectID,
		CommittedAt: committedAt,
	}).Error
}

// deleteTracking removes the row of objectID in the leaf repository repo. A missing row is not an error.
func (db *DB) deleteTracking(repo string, objectID *astral.ObjectID) error {
	return db.DB.
		Where("repository = ? AND object_id = ?", repo, objectID).
		Delete(&dbTracking{}).Error
}

// deleteRepositoryTracking removes every row of the leaf repository repo.
func (db *DB) deleteRepositoryTracking(repo string) error {
	return db.DB.
		Where("repository = ?", repo).
		Delete(&dbTracking{}).Error
}

// UpdateReadAt flushes a batch of pending read times. It only raises: a row keeps its stored read time
// when that time is not older than the batch time, and an ObjectID without a row changes nothing.
// A time sets the read time of every row with that ObjectID, in every repository.
// note: this is the only tracking query that does not filter by repository: one counted read touches every tracked copy (VFS 6, §11 "objects: last touched").
func (db *DB) UpdateReadAt(reads map[astral.ObjectID]astral.Time) error {
	if len(reads) == 0 {
		return nil
	}

	return db.DB.Session(&gorm.Session{PrepareStmt: true}).
		Transaction(func(tx *gorm.DB) error {
			for id, at := range reads {
				readAt := at.Time().Unix()
				err := tx.Model(&dbTracking{}).
					Where("object_id = ? AND read_at < ?", &id, readAt).
					Update("read_at", readAt).Error
				if err != nil {
					return err
				}
			}
			return nil
		})
}

// touchColumn is the last touch of a row: the later of its commit and read times.
const touchColumn = "MAX(committed_at, read_at)"

// untouchedCursor marks a position in the (last touch, object_id) order of one leaf.
type untouchedCursor struct {
	Touch    int64
	ObjectID *astral.ObjectID
}

// listUntouched returns up to limit rows of the leaf repository repo whose last touch is before cutoff,
// least recently touched first, starting after the cursor (nil = from the beginning).
// next is the cursor to resume from, or nil once the final page has been returned.
func (db *DB) listUntouched(repo string, cutoff int64, after *untouchedCursor, limit int) (rows []*dbTracking, next *untouchedCursor, err error) {
	q := db.DB.
		Where("repository = ? AND "+touchColumn+" < ?", repo, cutoff).
		Order(touchColumn + ", object_id").
		Limit(limit)
	if after != nil {
		q = q.Where("("+touchColumn+", object_id) > (?, ?)", after.Touch, after.ObjectID)
	}

	err = q.Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	// why: only a full page can have more rows after it.
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = &untouchedCursor{Touch: last.touch(), ObjectID: last.ObjectID}
	}

	return rows, next, nil
}

// findTracking returns the row of objectID in the leaf repository repo, or nil when there is none.
func (db *DB) findTracking(repo string, objectID *astral.ObjectID) (*dbTracking, error) {
	var rows []*dbTracking
	err := db.DB.
		Where("repository = ? AND object_id = ?", repo, objectID).
		Limit(1).
		Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
