package shell

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
)

var _ scheduler.Task = &LogAction{}

type LogAction struct {
	mod     *Module
	message string
}

func (l LogAction) String() string {
	return "shell.log_action"
}

func (l LogAction) Run(ctx *astral.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:

	}

	l.mod.log.Log(l.message)
	return nil
}
