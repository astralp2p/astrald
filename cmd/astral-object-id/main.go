package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/cryptopunkscc/astral-go/astral"
	"io"
	"os"
)

func main() {
	var verbose bool

	flag.BoolVar(&verbose, "v", false, "be verbose")
	flag.Parse()

	args := flag.Args()

	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-v] <file>\n", os.Args[0])
		os.Exit(1)
	}

	var input io.Reader
	var err error

	if args[0] == "-" {
		input = os.Stdin
	} else {
		input, err = os.Open(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
	}

	objectID, err := astral.Resolve(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(3)
	}

	if verbose {
		fmt.Println("id:  ", objectID.String())
		fmt.Println("size:", objectID.Size)
		fmt.Println("hash:", hex.EncodeToString(objectID.Hash[:]))
	} else {
		fmt.Println(objectID.String())
	}
}
