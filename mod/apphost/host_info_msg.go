package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type HostInfoMsg struct {
	Identity *astral.Identity
	Alias    astral.String8
}

func (HostInfoMsg) ObjectType() string {
	return "mod.apphost.host_info_msg"
}

func (msg HostInfoMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *HostInfoMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&HostInfoMsg{})
}
