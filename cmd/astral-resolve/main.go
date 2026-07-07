package main

import (
	"fmt"
	"os"

	dircli "github.com/cryptopunkscc/astral-go/api/dir/client"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: astral-resolve <name>")
		return
	}

	var ctx = astrald.NewContext()

	identity, err := dircli.ResolveIdentity(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}

	fmt.Println(identity)
}
