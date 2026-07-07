package shell

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
)

type Prompt struct {
	guestID *astral.Identity
	hostID  *astral.Identity
}

func (p Prompt) Render() string {
	return fmt.Sprintf("%v@%v> ", p.guestID, p.hostID)
}

func (Prompt) ObjectType() string {
	return ""
}

func (p Prompt) WriteTo(w io.Writer) (n int64, err error) {
	return 0, nil
}

func (p Prompt) ReadFrom(r io.Reader) (n int64, err error) {
	return 0, nil
}

func init() {
	_ = astral.Add(&Prompt{})
}
