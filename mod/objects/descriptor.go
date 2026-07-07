package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type Descriptor struct {
	SourceID *astral.Identity
	ObjectID *astral.ObjectID
	Data     astral.Object
}

func (res Descriptor) ObjectType() string {
	return "mod.objects.describe_result"
}

func (res Descriptor) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&res).WriteTo(w)
}

func (res *Descriptor) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(res).ReadFrom(r)
}

func (res Descriptor) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&res).MarshalJSON()
}

func (res *Descriptor) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(res).UnmarshalJSON(bytes)
}

func init() {
	astral.Add(&Descriptor{})
}
