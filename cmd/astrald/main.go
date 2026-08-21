package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/astralp2p/astrald/mod/all"
	_ "github.com/astralp2p/astrald/mod/all/views"
)

// Exit statuses
const (
	ExitSuccess   = iota // Normal exit
	ExitNodeError        // Node reported an error
	ExitForced           // User forced shutdown with double SIGINT
)

func main() {
	var args = parseArgs()

	if args.Version {
		os.Exit(printVersion())
	}

	// set up node execution context
	ctx, shutdown := context.WithCancel(context.Background())

	// trap ctrl+c
	sigCh := make(chan os.Signal)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "shutting down...")
		shutdown()

		<-sigCh
		fmt.Fprintln(os.Stderr, "forcing shutdown...")
		os.Exit(ExitForced)
	}()

	// run the node
	if err := run(ctx, args); err != nil {
		fmt.Fprintf(os.Stderr, "node error: %s\n", err)
		os.Exit(ExitNodeError)
	}

	os.Exit(ExitSuccess)
}
