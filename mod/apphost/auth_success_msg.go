package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// AuthSuccessMsg represents a success response to authentication methods.
type AuthSuccessMsg struct {
	GuestID *astral.Identity
}

var _ astral.Object = &AuthSuccessMsg{}

func (AuthSuccessMsg) ObjectType() string { return "mod.apphost.auth_success_msg" }

func (msg AuthSuccessMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *AuthSuccessMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&AuthSuccessMsg{})
}
