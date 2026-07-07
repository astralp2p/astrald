package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

// Run starts the node, waits for it to finish and returns an error if any
func (node *Node) Run(nctx context.Context) (err error) {
	ctx, shutdown := astral.NewContext(nctx).WithIdentity(node.identity).WithCancel()

	// Say hello
	node.log.Log("astral node %v (%v) starting...",
		node.identity,
		node.identity.String(),
	)

	// set this node as the default router for lib/astrald
	astrald.SetDefault(astrald.New(&routerAdapter{
		Router:   node,
		identity: node.identity,
	}))

	var wg sync.WaitGroup
	var errCh = make(chan error, 32)

	// run the module manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := node.modules.Run(ctx); err != nil {
			errCh <- fmt.Errorf("module manager: %w", err)
		}
	}()

	// wait for the context to end or a node error
	go func() {
		select {
		case <-ctx.Done():
		case err = <-errCh:
			shutdown()
		}
	}()

	node.startedAt = time.Now()

	// wait for all components to finish
	wg.Wait()

	time.Sleep(100 * time.Millisecond) // wait a little bit of time for all the buffers to flush

	return
}
