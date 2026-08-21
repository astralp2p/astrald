package scheduler

import "github.com/astralp2p/astral-go/astral"

// TaskFunc is a function that can be scheduled using Func()
//
//	mod.Scheduler.Schedule(scheduler.Func("test", func(ctx *astral.Context) error {
//	  // inline task here
//	  return nil
//	}))
type TaskFunc func(*astral.Context) error

type FuncAdapter struct {
	run  func(*astral.Context) error
	name string
}

var _ Task = &FuncAdapter{}

// Func wraps a function into a Task. The name argument is returned by String().
func Func(name string, run func(*astral.Context) error) *FuncAdapter {
	return &FuncAdapter{run: run, name: name}
}

func (t *FuncAdapter) Run(context *astral.Context) error {
	return t.run(context)
}

func (t *FuncAdapter) String() string {
	return t.name
}
