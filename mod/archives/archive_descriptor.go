package archives

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &ArchiveDescriptor{}

type ArchiveDescriptor struct {
	Format    astral.String8
	Entries   astral.Uint32
	TotalSize astral.Uint64
}

func (ArchiveDescriptor) ObjectType() string {
	return "mod.archives.archive_descriptor"
}

func (d ArchiveDescriptor) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&d).WriteTo(w)
}

func (d *ArchiveDescriptor) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(d).ReadFrom(r)
}

func (d *ArchiveDescriptor) String() string {
	return d.ObjectType()
}

func init() {
	astral.Add(&ArchiveDescriptor{})
}
