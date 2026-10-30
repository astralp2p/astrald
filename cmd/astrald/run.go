package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/debug"
	"github.com/astralp2p/astrald/resources"
)

const resNodeKey = "node_key"

func run(ctx context.Context, args *Args) error {
	nodeRes, err := setupResources(args)
	if err != nil {
		return err
	}

	// why: the crash-log directory is set by setupResources, so the handler is registered after it returns
	defer debug.SaveLog(func(p any) {
		debug.SigInt(p)
		time.Sleep(time.Second) // give components time to exit cleanly
	})

	nodeID, err := loadNodeIdentity(nodeRes)
	if err != nil {
		return err
	}

	// run the node
	coreNode, err := core.NewNode(nodeID, nodeRes)
	if err != nil {
		return err
	}

	return coreNode.Run(ctx)
}

func setupResources(args *Args) (resources.Resources, error) {
	if args.Ghost {
		return resources.NewMemResources(), nil
	}

	configRoot, dataRoot, err := nodeRoots(args.Root)
	if err != nil {
		return nil, err
	}

	nodeRes, err := resources.NewFileResources(configRoot, true)
	if err != nil {
		return nil, err
	}

	if err = nodeRes.SetDataRoot(dataRoot); err != nil {
		return nil, err
	}

	// make sure root directories exist
	err = os.MkdirAll(configRoot, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating config directory: %s\n", err)
	}

	err = os.MkdirAll(dataRoot, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating data directory: %s\n", err)
		return nil, err
	}

	// set directory for saving crash logs
	if err = debug.SetLogDir(dataRoot); err != nil {
		return nil, err
	}

	if err = moveNodeKey(nodeRes); err != nil {
		return nil, err
	}

	return nodeRes, nil
}

// moveNodeKey moves a node_key that an earlier version wrote to the config root into the data root.
// A copy left in the config root is a warning; the data-root key is in use.
func moveNodeKey(res *resources.FileResources) error {
	err := res.MoveToDataRoot(resNodeKey)
	if errors.Is(err, resources.ErrRootCopyKept) {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		return nil
	}

	return err
}
