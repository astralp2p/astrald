package vfs

import (
	"bytes"
	"errors"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// The sizes of a directory: the header, and an entry without its name.
const (
	dirHeaderSize = 26
	dirEntrySize  = 59
)

// errPassFull ends the planning of a pass whose budget is spent.
var errPassFull = errors.New("directory pass budget spent")

// plannedDir is the directory of one cleared folder, encoded under the lock of the VFS, to be stored and written
// back without it.
type plannedDir struct {
	Row      int64
	Gen      int64 // the generation of the folder when it was read
	ID       astral.ObjectID
	Bytes    []byte
	Children []int // the planned folders directly below, as indexes in dirPlan.Dirs
}

// dirPlan is the directories one compute stores, folders below first.
//
// Done reports whether the planned folder itself is planned; then ID is its ObjectID. Without a budget a plan is
// always done. A plan of a cached folder has no directories.
type dirPlan struct {
	VFS  *astral.Identity
	ID   astral.ObjectID
	Done bool
	Dirs []plannedDir
}

// dirPlanner encodes the directories of cleared folders bottom-up: the folders below first, then the folder.
//
// note: it only reads, so it belongs in one read transaction under the lock of the VFS. It calls no repository.
type dirPlanner struct {
	tx     *DB
	budget int // bytes of directories a pass plans at most; 0 means no budget
	used   int
	dirs   []plannedDir
}

// planFolder plans the directories a folder's ObjectID needs. A budget above 0 bounds one pass: a folder is
// planned only with every cleared folder below it, and planning stops once the planned bytes reach the budget.
//
// note: the first folder reached is always planned, so every pass makes progress.
func planFolder(tx *DB, vfsID *astral.Identity, row *dbEntry, budget int) (*dirPlan, error) {
	p := dirPlanner{tx: tx, budget: budget}
	plan := &dirPlan{VFS: vfsID}

	id, _, err := p.plan(row)
	switch {
	case errors.Is(err, errPassFull):
	case err != nil:
		return nil, err
	default:
		plan.ID, plan.Done = id, true
	}
	plan.Dirs = p.dirs

	return plan, nil
}

// plan returns the ObjectID of the directory of the folder in row and its index in the plan. A folder with a
// cached ObjectID is not read again and has the index -1.
func (p *dirPlanner) plan(row *dbEntry) (astral.ObjectID, int, error) {
	if row.ObjectID != nil {
		return *row.ObjectID, -1, nil
	}
	if p.budget > 0 && p.used >= p.budget {
		return astral.ObjectID{}, -1, errPassFull
	}

	children, err := p.tx.listChildren(row.ID)
	if err != nil {
		return astral.ObjectID{}, -1, err
	}

	kids, err := p.planChildren(children)
	if err != nil {
		return astral.ObjectID{}, -1, err
	}

	return p.encode(row, children, kids)
}

// planChildren plans the folders among children and sets their ObjectIDs. It returns the indexes of the planned
// ones.
func (p *dirPlanner) planChildren(children []*dbEntry) (kids []int, err error) {
	for _, child := range children {
		if !child.isFolder() {
			continue
		}

		id, index, err := p.plan(child)
		if err != nil {
			return nil, err
		}
		child.ObjectID = &id

		if index >= 0 {
			kids = append(kids, index)
		}
	}

	return kids, nil
}

// encode encodes the directory of a folder whose children have their ObjectIDs, and adds it to the plan.
func (p *dirPlanner) encode(row *dbEntry, children []*dbEntry, kids []int) (astral.ObjectID, int, error) {
	var buf bytes.Buffer

	dir, err := vfsapi.NewDirectoryWriter(&buf, uint32(len(children)))
	if err != nil {
		return astral.ObjectID{}, -1, err
	}

	for _, child := range children {
		if err = dir.Write(child.toEntry()); err != nil {
			return astral.ObjectID{}, -1, err
		}
	}

	id, err := dir.Close()
	if err != nil {
		return astral.ObjectID{}, -1, err
	}

	p.used += buf.Len()
	p.dirs = append(p.dirs, plannedDir{Row: row.ID, Gen: row.DirGen, ID: *id, Bytes: buf.Bytes(), Children: kids})

	return *id, len(p.dirs) - 1, nil
}

// folderObjectID returns the ObjectID of the directory of a folder. A cleared one is computed, stored and cached
// at once, without tokens. A compute of the same VFS that runs is waited for, and its result is used.
//
// A failed store is logged; the ObjectID is still answered and the folder stays cleared.
//
// note: a caller must not hold the lock of the VFS, nor be inside a transaction; the computation takes both.
func (mod *Module) folderObjectID(vfsID *astral.Identity, folderID int64) (astral.ObjectID, error) {
	row, err := mod.db.findEntry(folderID)
	if err != nil {
		return astral.ObjectID{}, err
	}
	if row.ObjectID != nil {
		return *row.ObjectID, nil
	}

	unlock := mod.lockCompute(vfsID)
	defer unlock()

	var plan *dirPlan

	// why: the row is read again under the locks, so a compute that ran meanwhile answers with what it cached.
	err = mod.runLockedTx(vfsID, func(tx *DB) error {
		row, err := tx.findEntry(folderID)
		if err != nil {
			return err
		}

		plan, err = planFolder(tx, vfsID, row, 0)

		return err
	})
	if err != nil {
		return astral.ObjectID{}, err
	}

	mod.storePlanLogged(plan)

	return plan.ID, nil
}
