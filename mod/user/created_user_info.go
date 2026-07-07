package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &CreatedUserInfo{}

type CreatedUserInfo struct {
	ID          *astral.Identity
	Alias       astral.String8
	KeyID       *astral.ObjectID
	ContractID  *astral.ObjectID
	Contract    *auth.SignedContract
	AccessToken astral.String8
}

func (s CreatedUserInfo) ObjectType() string {
	return "mod.users.created_user_info"
}

func (s CreatedUserInfo) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *CreatedUserInfo) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(s).ReadFrom(r)
}

func init() {
	_ = astral.Add(&CreatedUserInfo{})
}
