package fs

import (
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &EventFileChanged{}

type EventFileChanged struct {
	Path  astral.String16
	OldID *astral.ObjectID
	NewID *astral.ObjectID
}

func (EventFileChanged) ObjectType() string { return "astrald.mod.fs.events.file_changed" }

func (e EventFileChanged) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *EventFileChanged) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e EventFileChanged) String() string {
	return fmt.Sprintf("changed %s (%s -> %s)", e.Path, e.OldID, e.NewID)
}

var _ astral.Object = &EventFileAdded{}

type EventFileAdded struct {
	Path     astral.String16
	ObjectID *astral.ObjectID
}

func (EventFileAdded) ObjectType() string { return "astrald.mod.fs.events.file_added" }

func (e EventFileAdded) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *EventFileAdded) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e EventFileAdded) String() string {
	return fmt.Sprintf("added %s (%s)", e.Path, e.ObjectID)
}

var _ astral.Object = &EventFileRemoved{}

type EventFileRemoved struct {
	Path     astral.String16
	ObjectID *astral.ObjectID
}

func (EventFileRemoved) ObjectType() string { return "astrald.mod.fs.events.file_removed" }

func (e EventFileRemoved) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *EventFileRemoved) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e EventFileRemoved) String() string {
	return fmt.Sprintf("removed %s (%s)", e.Path, e.ObjectID)
}

func init() {
	_ = astral.Add(&EventFileAdded{})
	_ = astral.Add(&EventFileChanged{})
	_ = astral.Add(&EventFileRemoved{})
}
