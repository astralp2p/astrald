package nodes

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/nodes"
)

type NodeLinker struct {
	mod        *Module
	Target     *astral.Identity
	strategies sig.Map[string, nodes.LinkStrategy]
}

func NewNodeLinker(mod *Module, target *astral.Identity) *NodeLinker {
	linker := &NodeLinker{
		mod:    mod,
		Target: target,
	}

	for network, factory := range mod.strategyFactories.Clone() {
		linker.strategies.Set(network, factory.Build(target))
	}

	return linker
}

// Activate joins the named strategies' rounds. It returns a channel closed once
// all of them finish or ctx ends, an already-closed channel if none match, and
// the caller's idempotent release of every round it joined.
func (linker *NodeLinker) Activate(ctx *astral.Context, strategies []string) (done <-chan struct{}, release func()) {
	var doneChannels []<-chan struct{}
	var releases []func()
	for _, strategy := range strategies {
		if strategy, ok := linker.strategies.Get(strategy); ok {
			roundDone, leave := strategy.Join(ctx)
			releases = append(releases, leave)
			doneChannels = append(doneChannels, roundDone)
		}
	}

	release = func() {
		for _, r := range releases {
			r()
		}
	}

	if len(doneChannels) == 0 {
		doneCh := make(chan struct{})
		close(doneCh)
		return doneCh, release
	}

	return sig.WaitAllDone(ctx, doneChannels...), release
}

// rowsListener is a strategy whose running round dials rows stored after the round started.
type rowsListener interface {
	EndpointsAdded()
}

// EndpointsAdded tells every strategy that dials rows mid-round that new rows of Target were stored.
func (linker *NodeLinker) EndpointsAdded() {
	for _, strategy := range linker.strategies.Clone() {
		if l, ok := strategy.(rowsListener); ok {
			l.EndpointsAdded()
		}
	}
}
