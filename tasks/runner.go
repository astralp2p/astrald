package tasks

import (
	"errors"
	"github.com/cryptopunkscc/astral-go/astral"
	"sync"
)

// Runner is an interface that wraps the basic Run method.
// Run runs a task within the provided context and returns an error.
type Runner interface {
	Run(*astral.Context) error
}

type RunFunc func(*astral.Context) error

func Func(fn RunFunc) *FuncRunner {
	return &FuncRunner{Func: fn}
}

func Run(ctx *astral.Context, runners ...RunFunc) error {
	if len(runners) == 0 {
		return nil
	}

	var errs = make([]error, 0, len(runners))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range runners {
		r := r
		if r == nil {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			var err = r(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	return errors.Join(errs...)
}

type FuncRunner struct {
	Func RunFunc
}

func (r FuncRunner) Run(ctx *astral.Context) error {
	if r.Func == nil {
		panic("func is nil")
	}
	return r.Func(ctx)
}
