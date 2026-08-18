package shell

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

var _ scheduler.Task = &LogTask{}

type LogTask struct {
	mod     *Module
	message string
}

func (l LogTask) String() string {
	return "shell.log_task"
}

func (l LogTask) Run(ctx *astral.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:

	}

	l.mod.log.Log(l.message)
	return nil
}
