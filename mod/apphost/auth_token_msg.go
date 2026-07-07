package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// AuthTokenMsg represents an authentication request using an access token.
type AuthTokenMsg struct {
	Token astral.String8
}

func (AuthTokenMsg) ObjectType() string { return "mod.apphost.auth_token_msg" }

func (msg AuthTokenMsg) WriteTo(w io.Writer) (n int64, err error) {
	return msg.Token.WriteTo(w)
}

func (msg *AuthTokenMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return msg.Token.ReadFrom(r)
}

func init() {
	_ = astral.Add(&AuthTokenMsg{})
}
