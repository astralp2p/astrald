package vfs

import (
	"errors"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
	"gorm.io/gorm/clause"
)

// note: every query here loads its rows with Find before it returns. The node has one database connection, so a
// cursor left open across a repository call or a holder lookup would block the node.

// dbReplacedDirectory notes a directory ObjectID that a folder no longer has, for the sweeper to delete.
type dbReplacedDirectory struct {
	ID       int64            `gorm:"primaryKey"`
	ObjectID *astral.ObjectID `gorm:"not null;uniqueIndex"`
	NotedAt  int64            `gorm:"not null;index"` // nanoseconds since the Unix epoch, from the module's clock
}

func (dbReplacedDirectory) TableName() string { return vfsmod.DBPrefix + "replaced_directories" }

// dbDirGen is the folder generation counter of the node: one row that only goes up.
//
// A new row and each clear of folder IDs draw the next generation from it, in the transaction of the change.
// why: SQLite reuses the row id of a deleted last row, so a generation counted per row could repeat on a new
// folder; a node-wide counter never repeats, so a write-back matches only the folder its compute read.
type dbDirGen struct {
	ID   int64 `gorm:"primaryKey"`
	Last int64 `gorm:"not null"`
}

func (dbDirGen) TableName() string { return vfsmod.DBPrefix + "dir_gens" }

// errNoDirGen reports that the generation counter has no row; migrate seeds it.
var errNoDirGen = errors.New("folder generation counter missing")

// nextDirGen draws the next folder generation. The caller runs in the transaction of the row it is for.
func (db *DB) nextDirGen() (int64, error) {
	var gens []int64

	err := db.Raw(`UPDATE vfs__dir_gens SET last = last + 1 WHERE id = 1 RETURNING last`).Scan(&gens).Error
	switch {
	case err != nil:
		return 0, err
	case len(gens) == 0:
		return 0, errNoDirGen
	}

	return gens[0], nil
}

// seedDirGen makes the row of the generation counter when it is missing, at the highest generation of any row.
//
// why: the missing row is the marker, so the step runs once and a counter never goes down.
// why: the counter starts at the highest generation, so no draw repeats one an existing row has.
func (db *DB) seedDirGen() error {
	return db.Exec(`INSERT INTO vfs__dir_gens (id, last) SELECT 1, COALESCE(MAX(dir_gen), 0) FROM vfs__entries
		WHERE true ON CONFLICT (id) DO NOTHING`).Error
}

// noteReplaced notes directory ObjectIDs as replaced at now. A note that exists takes the new time.
func (db *DB) noteReplaced(ids []astral.ObjectID, now int64) error {
	if len(ids) == 0 {
		return nil
	}

	rows := make([]dbReplacedDirectory, 0, len(ids))
	for i := range ids {
		rows = append(rows, dbReplacedDirectory{ObjectID: &ids[i], NotedAt: now})
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "object_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"noted_at"}),
	}).Create(&rows).Error
}

// listReplacedBefore returns at most limit notes made at or before cutoff, in the order of their row, after the
// row afterID.
func (db *DB) listReplacedBefore(cutoff, afterID int64, limit int) (notes []dbReplacedDirectory, err error) {
	err = db.Where("noted_at <= ? AND id > ?", cutoff, afterID).Order("id").Limit(limit).Find(&notes).Error

	return
}

// claimReplaced deletes a note unless it was noted again since it was read, and reports whether it deleted it.
func (db *DB) claimReplaced(note dbReplacedDirectory) (bool, error) {
	res := db.Where("id = ? AND noted_at = ?", note.ID, note.NotedAt).Delete(&dbReplacedDirectory{})

	return res.RowsAffected == 1, res.Error
}

// listPendingVolumes returns every VFS whose root ObjectID is cleared.
func (db *DB) listPendingVolumes() ([]*astral.Identity, error) {
	var vols []dbVolume

	err := db.Model(&dbVolume{}).
		Joins("JOIN vfs__entries e ON e.id = vfs__volumes.root_id").
		Where("e.object_id IS NULL").
		Find(&vols).Error
	if err != nil {
		return nil, err
	}

	ids := make([]*astral.Identity, 0, len(vols))
	for _, vol := range vols {
		ids = append(ids, vol.Identity)
	}

	return ids, nil
}

// estimateDirCost returns what computing a VFS would charge: the size of the directory of every cleared folder, at
// least minCharge each. A VFS with no cleared folder costs 0.
//
// note: the estimate counts each folder, so identical directories, such as empty folders, cost more here than the
// compute charges, which stores them once.
// why: the name is cast to a blob, because length() of text counts characters, not bytes.
func (db *DB) estimateDirCost(vfsID *astral.Identity, minCharge uint64) (uint64, error) {
	var sizes []uint64

	err := db.Raw(`SELECT ? + COALESCE(SUM(? + length(CAST(c.name AS BLOB))), 0)
		FROM vfs__entries f LEFT JOIN vfs__entries c ON c.parent_id = f.id AND c.id <> c.parent_id
		WHERE f.vfs = ? AND f.flags & ? != 0 AND f.object_id IS NULL
		GROUP BY f.id`, dirHeaderSize, dirEntrySize, vfsID, uint16(vfsapi.FlagFolder)).Scan(&sizes).Error
	if err != nil {
		return 0, err
	}

	var total uint64
	for _, size := range sizes {
		total += max(size, minCharge)
	}

	return total, nil
}

// isRootCached reports whether the root of a VFS has a cached ObjectID.
func (db *DB) isRootCached(vfsID *astral.Identity) (bool, error) {
	vol, err := db.findVolume(vfsID)
	if err != nil {
		return false, err
	}

	root, err := db.findEntry(vol.RootID)
	if err != nil {
		return false, err
	}

	return root.ObjectID != nil, nil
}
