package archives

import (
	"context"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "archives"
const DBPrefix = "archives__"

// Module indexes archive objects and evicts them from the index on demand.
type Module interface {
	Index(context.Context, *astral.ObjectID) (*Archive, error)
	Forget(objectID *astral.ObjectID) error
}

// Entry is one file of an indexed archive.
//
// why: Entry and Archive are registered types, so EventArchiveIndexed has a Blueprint.
// why: Modified is astral.Time. A time.Time field encoded to no bytes, so the time never crossed the wire.
type Entry struct {
	ObjectID *astral.ObjectID
	Path     astral.String32
	Comment  astral.String32
	Modified astral.Time
}

func (Entry) ObjectType() string { return "mod.archives.entry" }

func (e Entry) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *Entry) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// Archive is the index of an archive object.
type Archive struct {
	Entries []*Entry
	Comment astral.String32
	Format  astral.String32
}

func (Archive) ObjectType() string { return "mod.archives.archive" }

func (a Archive) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *Archive) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() {
	_ = astral.Add(&Entry{})
	_ = astral.Add(&Archive{})
}
