package shell

import (
	"context"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestLogTask(t *testing.T) {
	l := log.New(astral.GenerateIdentity())
	l.SetFilter(func(*log.Entry) bool { return false })
	mod := &Module{log: l}

	task := mod.NewLogTask("m")

	if got := task.String(); got != "shell.log_task" {
		t.Fatalf("String() = %q, want %q", got, "shell.log_task")
	}

	if err := task.Run(astral.NewContext(nil)); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()
	if err := task.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() on a canceled context = %v, want %v", err, context.Canceled)
	}
}
