package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Info contains information about user's node configuration
type Info struct {
	NodeAlias  astral.String8
	UserAlias  astral.String8
	ContractID *astral.ObjectID

	Contract *auth.SignedContract
}

func (i Info) ObjectType() string { return "mod.user.info" }

func (i Info) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&i).WriteTo(w)
}

func (i *Info) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(i).ReadFrom(r)
}

func init() {
	astral.Add(&Info{})
}
