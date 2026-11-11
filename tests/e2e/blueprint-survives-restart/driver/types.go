package main

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// The two types no SDK ships. Their blueprints live on the node only while the
// node's process runs: the app derives them from these Go types and pushes them
// again whenever it (re)attaches.
const (
	noteType = "blueprint_survives_restart.note"
	tagType  = "blueprint_survives_restart.tag"
)

// Note is a record with a string and a wide integer.
type Note struct {
	Label  astral.String16
	Serial astral.Uint64
}

// astral:blueprint-ignore registered by registerTypes, in the app process only
func (*Note) ObjectType() string { return noteType }

func (o Note) WriteTo(w io.Writer) (int64, error) { return astral.Objectify(&o).WriteTo(w) }

func (o *Note) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(o).ReadFrom(r) }

// Tag is a record with a string and a narrow integer.
type Tag struct {
	Name astral.String16
	Rank astral.Uint8
}

// astral:blueprint-ignore registered by registerTypes, in the app process only
func (*Tag) ObjectType() string { return tagType }

func (o Tag) WriteTo(w io.Writer) (int64, error) { return astral.Objectify(&o).WriteTo(w) }

func (o *Tag) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(o).ReadFrom(r) }

// registerTypes adds the types to this process's compile-time registry, which
// is what apps.SyncBlueprints derives the blueprints it pushes from.
func registerTypes() error { return astral.Add(&Note{}, &Tag{}) }
