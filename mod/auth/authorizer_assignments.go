package auth

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizerAssignments is the node's persisted set of external authorizer
// assignments, held as one tree value at /mod/auth/authorizers.
type AuthorizerAssignments struct {
	Assignments []*auth.AuthorizerAssignment
}

func (AuthorizerAssignments) ObjectType() string { return "mod.auth.authorizer_assignments" }

func (a AuthorizerAssignments) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *AuthorizerAssignments) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() { _ = astral.Add(&AuthorizerAssignments{}) }
