package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// ReadObjectAction requests permission to read the object identified by ObjectID.
type ReadObjectAction struct {
	auth.Action
	ObjectID *astral.ObjectID
}

func (ReadObjectAction) ObjectType() string { return "mod.objects.read_object_action" }

func (a ReadObjectAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *ReadObjectAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() { _ = astral.Add(&ReadObjectAction{}) }
