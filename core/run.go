package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/resources"
)

// Run starts the node, waits for it to finish and returns an error if any
func (node *Node) Run(nctx context.Context) (err error) {
	ctx, shutdown := astral.NewContext(nctx).WithIdentity(node.identity).WithCancel()

	// Say hello
	node.log.Log("astral node %v (%v) starting...",
		node.identity,
		node.identity.String(),
	)

	if res, ok := node.assets.Res().(*resources.FileResources); ok {
		node.log.Log("config root: %v", res.Root())
		node.log.Log("data root: %v", res.DataRoot())
	}

	// set this node as the default router for lib/astrald
	astrald.SetDefault(NewClientAs(node, node.identity))

	var wg sync.WaitGroup
	var errCh = make(chan error, 32)

	// run the module manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := node.modules.Run(ctx); err != nil {
			errCh <- fmt.Errorf("module manager: %w", err)
			shutdown()
		}
	}()

	node.startedAt = time.Now()

	// wait for all components to finish
	wg.Wait()

	// why after wg.Wait: the error is read once every sender is done, so a node error is
	// returned even when the context ended first.
	select {
	case err = <-errCh:
	default:
	}

	time.Sleep(100 * time.Millisecond) // wait a little bit of time for all the buffers to flush

	return
}
