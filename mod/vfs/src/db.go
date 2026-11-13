package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

func (db *DB) migrate() error {
	if err := db.renameEntryLimit(); err != nil {
		return err
	}

	if err := db.migrateCreationTime(); err != nil {
		return err
	}

	if err := db.migrateDirGen(); err != nil {
		return err
	}

	if err := db.AutoMigrate(&dbVolume{}, &dbEntry{}, &dbReplacedDirectory{}, &dbDirGen{}); err != nil {
		return err
	}

	return db.seedDirGen()
}

// migrateDirGen adds the dir_gen column to an entries table made before it. In the same transaction every cached
// folder ObjectID is cleared.
//
// why: the missing column is the marker, so the step runs once; one transaction adds the column and clears the
// IDs, so a failed step leaves no column and runs again on the next start.
// why: a folder ObjectID cached before this version names no stored object; cleared, it is computed and stored.
// note: nothing is noted for removal, because none of those IDs was stored.
func (db *DB) migrateDirGen() error {
	m := db.Migrator()
	if !m.HasTable(&dbEntry{}) || m.HasColumn(&dbEntry{}, "dir_gen") {
		return nil
	}

	return db.inTx(func(tx *DB) error {
		if err := tx.Migrator().AddColumn(&dbEntry{}, "DirGen"); err != nil {
			return err
		}

		return tx.Exec(`UPDATE vfs__entries SET object_id = NULL WHERE flags & ? != 0`, uint16(vfsapi.FlagFolder)).Error
	})
}

// migrateCreationTime adds the creation_time column to an entries table made before it. In the same transaction
// every entry gets its modification time as its creation time, and every cached folder ObjectID is cleared.
//
// why: the missing column is the marker, so the step runs once; one transaction adds the column with the rows'
// values, so a failed step leaves no column and runs again on the next start.
// why: the folder ObjectIDs are cleared because the directory type name and the entry layout changed with the
// column.
// note: the root has no creation time and keeps 0.
func (db *DB) migrateCreationTime() error {
	m := db.Migrator()
	if !m.HasTable(&dbEntry{}) || m.HasColumn(&dbEntry{}, "creation_time") {
		return nil
	}

	return db.inTx(func(tx *DB) error {
		if err := tx.Migrator().AddColumn(&dbEntry{}, "CreationTime"); err != nil {
			return err
		}

		err := tx.Exec(`UPDATE vfs__entries SET creation_time = mod_time WHERE id <> parent_id`).Error
		if err != nil {
			return err
		}

		return tx.Exec(`UPDATE vfs__entries SET object_id = NULL WHERE flags & ? != 0`, uint16(vfsapi.FlagFolder)).Error
	})
}

// renameEntryLimit renames the column entry_limit of a volumes table made before the inode names to inode_limit.
//
// why: AutoMigrate would add inode_limit beside entry_limit, and SQLite refuses a new not null column without a default.
func (db *DB) renameEntryLimit() error {
	m := db.Migrator()
	if !m.HasColumn(&dbVolume{}, "entry_limit") {
		return nil
	}

	return m.RenameColumn(&dbVolume{}, "entry_limit", "inode_limit")
}

// inTx runs fn in one transaction and commits it when fn returns nil.
func (db *DB) inTx(fn func(tx *DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error { return fn(&DB{tx}) })
}

// findVolume returns the VFS of an identity, or vfsapi.ErrNoSuchVFS.
func (db *DB) findVolume(id *astral.Identity) (*dbVolume, error) {
	var rows []dbVolume

	// why: Find with a limit, not First, so a miss logs no "record not found" line.
	err := db.Where("identity = ?", id).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNoSuchVFS
	}

	return &rows[0], nil
}

// listVolumes returns every VFS in the order of creation.
func (db *DB) listVolumes() (volumes []*dbVolume, err error) {
	err = db.Order("id").Find(&volumes).Error

	return
}

// createVolume inserts a VFS with its root folder. The root is its own parent.
func (db *DB) createVolume(id *astral.Identity, byteLimit, inodeLimit uint64) error {
	root := &dbEntry{
		VFS:     id,
		Flags:   uint16(vfsapi.FlagFolder),
		ModTime: nodeTime(),
	}

	if err := db.insertEntry(root); err != nil {
		return err
	}

	root.ParentID = root.ID
	if err := db.Model(root).Update("parent_id", root.ID).Error; err != nil {
		return err
	}

	return db.Create(&dbVolume{
		Identity:   id,
		RootID:     root.ID,
		ByteLimit:  limitColumn(byteLimit),
		InodeLimit: limitColumn(inodeLimit),
	}).Error
}

// setVolumeLimits writes the byte limit and the inode limit of a VFS.
func (db *DB) setVolumeLimits(volID int64, byteLimit, inodeLimit uint64) error {
	return db.Model(&dbVolume{}).Where("id = ?", volID).Updates(map[string]any{
		"byte_limit":  limitColumn(byteLimit),
		"inode_limit": limitColumn(inodeLimit),
	}).Error
}

// setVolumeEvaluator writes the evaluator endpoint of a VFS. An empty endpoint clears it.
func (db *DB) setVolumeEvaluator(volID int64, endpoint string) error {
	return db.Model(&dbVolume{}).Where("id = ?", volID).Update("evaluator", endpoint).Error
}

// findEntry returns the row of an entry, or vfsapi.ErrNotFound.
func (db *DB) findEntry(rowID int64) (*dbEntry, error) {
	var rows []dbEntry

	err := db.Where("id = ?", rowID).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNotFound
	}

	return &rows[0], nil
}

// findChild returns the entry called name in a folder, or vfsapi.ErrNotFound.
func (db *DB) findChild(parentID int64, name string) (*dbEntry, error) {
	var rows []dbEntry

	// note: the root is its own parent and has an empty name, which no valid name equals.
	err := db.Where("parent_id = ? AND name = ?", parentID, name).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNotFound
	}

	return &rows[0], nil
}

// listChildren returns the entries of a folder in ascending byte order of name.
func (db *DB) listChildren(parentID int64) (rows []*dbEntry, err error) {
	// note: the root is its own parent, so the query leaves the root row itself out.
	err = db.Where("parent_id = ? AND id <> parent_id", parentID).Order("name").Find(&rows).Error

	return
}

// hasChildren reports whether a folder has at least one entry.
func (db *DB) hasChildren(parentID int64) (bool, error) {
	var n int64

	// note: the root is its own parent, so the query leaves the root row itself out.
	err := db.Model(&dbEntry{}).Where("parent_id = ? AND id <> parent_id", parentID).Count(&n).Error

	return n > 0, err
}

// resolveEntry walks the names from the root of a VFS and returns the row they name. No names give the root row.
//
// A missing name answers vfsapi.ErrNotFound. A file in the middle of the walk answers vfsapi.ErrNotAFolder. The
// last name may be a file or a folder.
func (db *DB) resolveEntry(vol *dbVolume, names []string) (*dbEntry, error) {
	row, err := db.findEntry(vol.RootID)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		if !row.isFolder() {
			return nil, vfsapi.ErrNotAFolder
		}

		if row, err = db.findChild(row.ID, name); err != nil {
			return nil, err
		}
	}

	return row, nil
}

// insertEntry inserts a new row at the next folder generation.
//
// why: a file row draws one too, so a row id reused by any row never meets a generation a compute read.
func (db *DB) insertEntry(row *dbEntry) (err error) {
	if row.DirGen, err = db.nextDirGen(); err != nil {
		return err
	}

	return db.Create(row).Error
}

// setEntryTime sets the modification time of one row.
func (db *DB) setEntryTime(rowID int64, modTime int64) error {
	return db.Model(&dbEntry{}).Where("id = ?", rowID).Update("mod_time", modTime).Error
}

// setEntryParent writes the parent and the name of one row.
func (db *DB) setEntryParent(rowID, parentID int64, name string) error {
	return db.Model(&dbEntry{}).Where("id = ?", rowID).
		Updates(map[string]any{"parent_id": parentID, "name": name}).Error
}

// setFileContent writes the content ObjectID and the modification time of one row.
func (db *DB) setFileContent(rowID int64, id *astral.ObjectID, modTime int64) error {
	return db.Model(&dbEntry{}).Where("id = ?", rowID).
		Updates(map[string]any{"object_id": id, "mod_time": modTime}).Error
}

// subtreeCTE names the rows of a subtree: the row bound to its parameter and every row below it.
//
// note: the walk goes down from a row that is not a root, so it never reaches the root, which is its own parent.
const subtreeCTE = `WITH RECURSIVE tree(id) AS (
	SELECT id FROM vfs__entries WHERE id = ?
	UNION
	SELECT e.id FROM vfs__entries e JOIN tree t ON e.parent_id = t.id
) SELECT id FROM tree`

// ancestorsCTE names the rows of a path: the row bound to its parameter and every row above it, up to the root.
//
// note: the root is its own parent, and UNION drops a repeated row, so the recursion ends there.
const ancestorsCTE = `WITH RECURSIVE ancestors(id, parent_id) AS (
	SELECT id, parent_id FROM vfs__entries WHERE id = ?
	UNION
	SELECT e.id, e.parent_id FROM vfs__entries e JOIN ancestors a ON e.id = a.parent_id
) SELECT id FROM ancestors`

// deleteSubtree deletes a row and every row below it. The cached ObjectIDs of the folders it deletes are noted as
// replaced at now.
func (db *DB) deleteSubtree(rowID, now int64) error {
	if err := db.noteFolderIDsIn(subtreeCTE, rowID, now); err != nil {
		return err
	}

	return db.Exec(`DELETE FROM vfs__entries WHERE id IN (`+subtreeCTE+`)`, rowID).Error
}

// clearFolderIDs clears the cached ObjectID of a folder and of every folder above it, up to the root, and sets
// their generations to the next one of the node. The cleared ObjectIDs are noted as replaced at now.
//
// A change in a folder, or below it, calls this with the row of the folder. A file row is never cleared, so a
// call with the row of a file clears the folders above it only.
//
// why: the generation moves on an already cleared slot too, so a compute that read the folder before this
// change never writes back.
// why: a file's ObjectID names its content and is never a cache.
func (db *DB) clearFolderIDs(folderID, now int64) error {
	if err := db.noteFolderIDsIn(ancestorsCTE, folderID, now); err != nil {
		return err
	}

	gen, err := db.nextDirGen()
	if err != nil {
		return err
	}

	return db.Exec(`UPDATE vfs__entries SET object_id = NULL, dir_gen = ?
		WHERE flags & ? != 0 AND id IN (`+ancestorsCTE+`)`, gen, uint16(vfsapi.FlagFolder), folderID).Error
}

// noteFolderIDsIn notes the cached ObjectIDs of the folders among the rows a CTE names as replaced at now.
//
// note: the latest note wins, so the grace time counts from the last replacement.
// note: the WHERE clause also settles SQLite's parse of ON CONFLICT after a SELECT.
func (db *DB) noteFolderIDsIn(cte string, rowID, now int64) error {
	return db.Exec(`INSERT INTO vfs__replaced_directories (object_id, noted_at)
		SELECT object_id, ? FROM vfs__entries
		WHERE flags & ? != 0 AND object_id IS NOT NULL AND id IN (`+cte+`)
		ON CONFLICT (object_id) DO UPDATE SET noted_at = excluded.noted_at`,
		now, uint16(vfsapi.FlagFolder), rowID).Error
}

// setFolderIDIfUnchanged caches a computed ObjectID in its row when the folder is still cleared at the generation
// the compute read. It reports whether the row took the ID.
//
// note: a generation is drawn once on the node (dbDirGen), so the row id and the generation name one folder state.
func (db *DB) setFolderIDIfUnchanged(rowID, gen int64, id astral.ObjectID) (bool, error) {
	res := db.Model(&dbEntry{}).
		Where("id = ? AND dir_gen = ? AND object_id IS NULL", rowID, gen).
		Update("object_id", &id)

	return res.RowsAffected == 1, res.Error
}

// usage returns the bytes of file content a VFS holds. Each distinct ObjectID counts once, and a folder counts
// nothing.
//
// why: usage is computed from the entries, so no column can drift from them.
func (db *DB) usage(vfsID *astral.Identity) (total uint64, err error) {
	rows, err := db.Model(&dbEntry{}).
		Where("vfs = ? AND flags & ? = 0", vfsID, uint16(vfsapi.FlagFolder)).
		Distinct("object_id").
		Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id astral.ObjectID
		if err = rows.Scan(&id); err != nil {
			return 0, err
		}
		total += id.Size
	}

	return total, rows.Err()
}

// entryCount returns the number of entries of a VFS. The root is not counted.
func (db *DB) entryCount(vfsID *astral.Identity) (count uint64, err error) {
	var n int64

	err = db.Model(&dbEntry{}).Where("vfs = ? AND id <> parent_id", vfsID).Count(&n).Error

	return uint64(n), err
}

// vfsUsage is what the limits of a VFS measure.
type vfsUsage struct {
	Bytes  uint64
	Inodes uint64
}

// measureUsage returns the usage of a VFS.
func (db *DB) measureUsage(vfsID *astral.Identity) (u vfsUsage, err error) {
	if u.Bytes, err = db.usage(vfsID); err != nil {
		return
	}

	u.Inodes, err = db.entryCount(vfsID)

	return
}

// hasEntryWithObject reports whether a row has the ObjectID: the content of a file or the cached directory of a
// folder.
func (db *DB) hasEntryWithObject(objectID *astral.ObjectID) (bool, error) {
	var ids []int64

	err := db.Model(&dbEntry{}).Where("object_id = ?", objectID).Limit(1).Pluck("id", &ids).Error

	return len(ids) > 0, err
}

// findVFSesWithObject returns each VFS that has a file or a cached folder with the ObjectID, once.
//
// note: Find loads every row before the caller asks another module, so no cursor holds the one connection.
func (db *DB) findVFSesWithObject(id *astral.ObjectID) ([]*astral.Identity, error) {
	var rows []dbEntry

	err := db.Model(&dbEntry{}).Distinct("vfs").Where("object_id = ?", id).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	ids := make([]*astral.Identity, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.VFS)
	}

	return ids, nil
}
