package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		mem := resources.NewMemResources()
		mem.Write("log.yaml", []byte("level: 2"))
		return mem, nil
	}

	// derive config and data roots from a single base directory
	configRoot := defaultConfigRoot()
	dataRoot := defaultDataRoot()

	if args.Root != "" {
		configRoot = filepath.Join(args.Root, "config")
		dataRoot = filepath.Join(args.Root, "data")
	}

	nodeRes, err := resources.NewFileResources(configRoot, true)
	if err != nil {
		return nil, err
	}

	nodeRes.SetDataRoot(dataRoot)

	// make sure root directories exist
	err = os.MkdirAll(configRoot, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating config directory: %s\n", err)
	}

	err = os.MkdirAll(dataRoot, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating data directory: %s\n", err)
	}

	// set directory for saving crash logs
	debug.LogDir = configRoot
	defer debug.SaveLog(func(p any) {
		debug.SigInt(p)
		time.Sleep(time.Second) // give components time to exit cleanly
	})

	return nodeRes, err
}
