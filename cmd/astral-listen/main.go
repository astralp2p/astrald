package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	dircli "github.com/cryptopunkscc/astral-go/api/dir/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/apps"
	libastrald "github.com/cryptopunkscc/astral-go/lib/astrald"
)

func main() {
	var accept string
	flag.StringVar(&accept, "a", "", "accept query")
	flag.Parse()

	var ctx = libastrald.NewContext()

	handler, err := apps.NewHandler()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err = apps.NewDefaultAppRegistrar(ctx).Register(ctx, handler.Endpoint(), handler.Token()); err != nil {
		fmt.Fprintf(os.Stderr, "register: %v\n", err)
		os.Exit(1)
	}

	err = handler.Serve(ctx, func(ctx *astral.Context, query *apps.PendingQuery) error {
		caller, _ := dircli.GetAlias(ctx, query.Caller())
		if caller == "" {
			caller = query.Caller().String()
		}

		if query.Query() == accept {
			fmt.Fprintf(os.Stderr, "accepted [%s] %s from %s\n", query.Nonce(), query.Query(), caller)
			conn := query.Accept()

			go func() {
				defer conn.Close()
				io.Copy(conn, os.Stdin)
			}()

			io.Copy(os.Stdout, conn)
			os.Exit(0)
		}

		fmt.Fprintf(os.Stderr, "ignored [%s] %s from %s\n", query.Nonce(), query.Query(), caller)

		query.Skip()
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

}
