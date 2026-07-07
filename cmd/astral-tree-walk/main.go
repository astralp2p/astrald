package main

import (
	"encoding"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	dircli "github.com/cryptopunkscc/astrald/mod/dir/client"
	"github.com/cryptopunkscc/astrald/mod/tree"
	treecli "github.com/cryptopunkscc/astrald/mod/tree/client"
)

func walk(ctx *astral.Context, node tree.Node, path []string) error {
	fmt.Print(strings.Repeat("  ", len(path)))

	if len(path) > 0 {
		fmt.Print(path[len(path)-1])
	}

	object, err := tree.Get[astral.Object](ctx, node)
	if err == nil {
		var text []byte
		t := object.ObjectType()

		if t != "nil" {
			if m, ok := object.(encoding.TextMarshaler); ok {
				text, _ = m.MarshalText()
			} else {
				text = []byte("[data]")
			}

			fmt.Printf(" = \"%s\" [%s]", string(text), t)
		}
		fmt.Println()
	} else {
		fmt.Println()
	}

	sub, err := node.Sub(ctx)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(sub))
	for k := range sub {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, name := range keys {
		err = walk(ctx, sub[name], append(path, name))
		if err != nil {
			return err
		}
	}

	return nil
}

func main() {
	var err error
	var targetID *astral.Identity
	var target, path string

	ctx := astrald.NewContext()
	client := treecli.Default()

	// parse the args
	if len(os.Args) > 1 {
		parts := strings.SplitN(os.Args[1], ":", 2)
		target = parts[0]
		if len(parts) > 1 {
			path = parts[1]
		}
		targetID, err = dircli.ResolveIdentity(ctx, target)
		if err != nil {
			fatal("resolve identity: %v", err)
		}
		client = treecli.New(targetID, nil)
	}
	if target == "" {
		target = "localnode"
	}

	alias, err := dircli.GetAlias(ctx, targetID)
	if err == nil {
		fmt.Printf("%s %s", alias, path)
	} else {
		fmt.Printf("%s %s", target, path)
	}

	// query the node
	node := client.Root()
	if path != "" {
		node, err = tree.Query(ctx, node, path, false)
		if err != nil {
			fatal("query: %v", err)
		}
	}

	// walk the tree
	err = walk(ctx, node, nil)
	if err != nil {
		fatal("walk: %v", err)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
