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

// Activate signals the named strategies and returns a channel closed once all of them finish; an already-closed channel if none match.
func (linker *NodeLinker) Activate(ctx *astral.Context, strategies []string) <-chan struct{} {
	var doneChannels []<-chan struct{}
	for _, strategy := range strategies {
		if strategy, ok := linker.strategies.Get(strategy); ok {
			strategy.Signal(ctx)
			doneChannels = append(doneChannels, strategy.Done())
		}
	}

	if len(doneChannels) == 0 {
		doneCh := make(chan struct{})
		close(doneCh)
		return doneCh
	}

	return sig.WaitAllDone(ctx, doneChannels...)
}
