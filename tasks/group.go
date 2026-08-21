package tasks

import (
	"github.com/astralp2p/astral-go/astral"
)

type GroupRunner struct {
	runners []Runner
}

func Group(runners ...Runner) *GroupRunner {
	return &GroupRunner{runners: runners}
}

func (g *GroupRunner) Run(ctx *astral.Context) (err error) {
	var fns = make([]RunFunc, 0, len(g.runners))
	for _, r := range g.runners {
		fns = append(fns, r.Run)
	}
	return Run(ctx, fns...)
}
