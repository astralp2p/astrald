package objects

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func (db *DB) Migrate() error {
	return db.AutoMigrate(&dbObject{})
}

func (db *DB) Contains(id *astral.ObjectID) (b bool, err error) {
	err = db.
		Model(&dbObject{}).
		Where("id = ?", id).
		Select("count(*)>0").
		First(&b).Error
	return
}

func (db *DB) Find(id *astral.ObjectID) (row *dbObject, err error) {
	err = db.
		Where("id = ?", id).
		First(&row).Error
	return
}

// Create seeds a tracking row for id with the given type. Idempotent: if a
// row for id already exists, the call is a no-op (existing Type and ReadAt
// are preserved). Used by every "object entered the device" path —
// Module.Store/Load/Probe/GetType and OpCreate — to keep dbObject in sync
// with what flows through the module.
func (db *DB) Create(id *astral.ObjectID, objectType string) error {
	// why: an empty type means "unknown" (blob/raw create); store NULL, not "",
	// so GetType re-reads the stamp instead of returning a cached blank.
	var t *string
	if objectType != "" {
		t = &objectType
	}
	// why: read_at must match UpdateReadAt's UTC writes; mixing local time.Now()
	// here would corrupt the (read_at, height) purge order on non-UTC nodes.
	return db.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&dbObject{
		ID:     id,
		Type:   t,
		ReadAt: astral.Now().Time(),
	}).Error
}

// UpdateReadAt flushes a batch of pending read times. It is UPDATE-only: each
// id present in the table has its read_at bumped, and ids that aren't already
// tracked are silently skipped — first reads do NOT seed rows here.
func (db *DB) UpdateReadAt(reads map[astral.ObjectID]astral.Time) error {
	if len(reads) == 0 {
		return nil
	}

	return db.DB.Session(&gorm.Session{PrepareStmt: true}).
		Transaction(func(tx *gorm.DB) error {
			for id, at := range reads {
				err := tx.Model(&dbObject{}).
					Where("id = ?", &id).
					Update("read_at", at.Time()).Error
				if err != nil {
					return err
				}
			}
			return nil
		})
}

// readCursor marks a position in the (read_at, height) read order.
type readCursor struct {
	ReadAt time.Time
	Height uint64
}

// ListReadOldest returns up to limit object IDs ordered oldest-read first, starting
// after the cursor (nil = from the beginning). next is the cursor to resume
// from, or nil once the final page has been returned.
func (db *DB) ListReadOldest(after *readCursor, limit int) (ids []*astral.ObjectID, next *readCursor, err error) {
	if limit <= 0 {
		return nil, nil, nil
	}

	q := db.DB.Order("read_at, height").Limit(limit)
	if after != nil {
		// keyset seek: (read_at, height) is a total order since height is unique.
		// the row-value form maps directly onto the read_at index, whose entries
		// implicitly carry height (the rowid) as their tiebreaker.
		q = q.Where("(read_at, height) > (?, ?)", after.ReadAt, after.Height)
	}

	var rows []*dbObject
	err = q.Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	ids = make([]*astral.ObjectID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}

	// a short page means we reached the end; only hand back a cursor when the
	// page was full, so the caller knows there may be more
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = &readCursor{ReadAt: last.ReadAt, Height: last.Height}
	}

	return ids, next, nil
}

// DeleteObjectCacheByID removes the tracking row for id. Used by purge to drop
// rows whose repo object has gone missing; safe to call for an id with no row
// (no-op).
func (db *DB) DeleteObjectCacheByID(id *astral.ObjectID) error {
	return db.DB.Where("id = ?", id).Delete(&dbObject{}).Error
}

func (db *DB) FindByType(objectType string) (rows []*dbObject, err error) {
	err = db.
		Where("type = ?", objectType).
		Find(&rows).Error
	return
}
