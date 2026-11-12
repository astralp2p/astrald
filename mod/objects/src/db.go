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

// readCursor marks a position in the (last touch, repository, object_id) order.
type readCursor struct {
	Touch      int64
	Repository string
	ObjectID   *astral.ObjectID
}

// touchColumn is the last touch of a row: the later of its commit and read times.
const touchColumn = "MAX(committed_at, read_at)"

// ListReadOldest returns up to limit rows ordered least recently touched first, starting
// after the cursor (nil = from the beginning). An empty repo lists every repository.
// next is the cursor to resume from, or nil once the final page has been returned.
// todo: part 2c of the VFS ticket replaces this listing with the per-leaf purge of VFS 6, §11 "objects: purge".
func (db *DB) ListReadOldest(repo string, after *readCursor, limit int) (rows []*dbTracking, next *readCursor, err error) {
	if limit <= 0 {
		return nil, nil, nil
	}

	q := db.DB.Order(touchColumn + ", repository, object_id").Limit(limit)
	if repo != "" {
		q = q.Where("repository = ?", repo)
	}
	if after != nil {
		q = q.Where("("+touchColumn+", repository, object_id) > (?, ?, ?)", after.Touch, after.Repository, after.ObjectID)
	}

	err = q.Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	// a short page means we reached the end; only hand back a cursor when the
	// page was full, so the caller knows there may be more
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = &readCursor{Touch: max(last.CommittedAt, last.ReadAt), Repository: last.Repository, ObjectID: last.ObjectID}
	}

	return rows, next, nil
}
