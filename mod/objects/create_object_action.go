package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

type CreateObjectAction struct {
	auth.Action
}

func (CreateObjectAction) ObjectType() string { return "mod.objects.create_object_action" }

func (a CreateObjectAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *CreateObjectAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a CreateObjectAction) ApplyConstraints(_ *astral.Bundle) bool {
	return true
}

func init() { _ = astral.Add(&CreateObjectAction{}) }
