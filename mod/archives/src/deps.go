package archives

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type Deps struct {
	Auth    auth.Module
	Objects objectsmod.Module
}

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}
	mod.Auth.Add(auth.Func[*objects.ReadObjectAction](mod.AuthorizeObjectsRead))
	return
}
