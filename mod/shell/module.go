package shell

import (
	"github.com/astralp2p/astrald/mod/scheduler"
)

const ModuleName = "shell"

type Module interface {
	NewLogTask(message string) LogTask
}

type LogTask interface {
	scheduler.Task
}
