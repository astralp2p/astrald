package shell

import (
	"github.com/astralp2p/astrald/mod/scheduler"
)

const ModuleName = "shell"

type Module interface {
	NewLogAction(message string) LogAction
}

type LogAction interface {
	scheduler.Task
}
