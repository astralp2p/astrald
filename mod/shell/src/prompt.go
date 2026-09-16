package shell

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
)

// Prompt is the shell's command prompt. GuestID is the identity running the
// session; HostID is the node serving it.
//
// why: the fields are exported because astral.Objectify encodes through
// reflection and skips a field it cannot Interface(). Unexported identities
// would make the codec a no-op again.
type Prompt struct {
	GuestID *astral.Identity
	HostID  *astral.Identity
}

func (p Prompt) Render() string {
	return fmt.Sprintf("%v@%v> ", p.GuestID, p.HostID)
}

func (Prompt) ObjectType() string {
	return "mod.shell.prompt"
}

func (p Prompt) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&p).WriteTo(w)
}

func (p *Prompt) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(p).ReadFrom(r)
}

func init() {
	astral.MustAdd(&Prompt{})
}
