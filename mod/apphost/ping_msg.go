package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// PingMsg represents a ping request.
type PingMsg struct{}

func (p PingMsg) ObjectType() string {
	return "mod.apphost.ping_msg"
}

func (p PingMsg) WriteTo(w io.Writer) (n int64, err error) {
	return 0, nil
}

func (p PingMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return 0, nil
}

func init() {
	_ = astral.Add(&PingMsg{})
}
