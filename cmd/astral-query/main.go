package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/query"
	"github.com/cryptopunkscc/astrald/lib/arl"
	dircli "github.com/cryptopunkscc/astrald/mod/dir/client"
)

const (
	EnvDefaultTarget = "ASTRAL_DEFAULT_TARGET"
)

func main() {
	var zoneFlag string
	var filterFlag filterList

	flag.StringVar(&zoneFlag, "zone", "", "zones to include: any combination of d(evice), v(irtual), n(etwork)")
	flag.Var(&filterFlag, "filter", "identity `filter` to apply (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-zone dvn] [-filter name]... <query> [-arg <val>]...\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	// show help
	if flag.NArg() < 1 {
		flag.Usage()
		return
	}

	defaultIn := inputFormat()
	defaultOut := outputFormat()

	var err error
	var callerID, targetID *astral.Identity

	// split the argument into parts
	caller, target, method := arl.Split(flag.Arg(0))

	// create new astral context
	var ctx = astrald.NewContext()

	if zoneFlag != "" {
		ctx = ctx.WithZone(astral.Zones(zoneFlag))
	}
	if len(filterFlag) > 0 {
		ctx = ctx.WithFilters(filterFlag...)
	}

	// parse the caller
	if len(caller) > 0 {
		callerID, err = dircli.ResolveIdentity(ctx, caller)
		if err != nil {
			fatal("error: %v\n", err)
		}
	}

	// set default target if none given
	if len(target) == 0 {
		target = os.Getenv(EnvDefaultTarget)
	}

	// parse the target
	if len(target) > 0 {
		targetID, err = dircli.ResolveIdentity(ctx, target)
		if err != nil {
			fatal("error: %v\n", err)
		}
	}

	args := parseQueryArgs(flag.Args()[1:])

	// set default input/output formats
	if defaultIn != "" && args["in"] == "" {
		args["in"] = defaultIn
	}
	if defaultOut != "" && args["out"] == "" {
		args["out"] = defaultOut
	}

	// route the query
	conn, err := astrald.RouteQuery(ctx, astral.Launch(query.New(callerID, targetID, method, args)))
	if err != nil {
		fatal("error: %v\n", err)
	}

	// join conn with the terminal
	go func() {
		io.Copy(conn, os.Stdin)
	}()

	io.Copy(os.Stdout, conn)
}

func parseQueryArgs(a []string) map[string]string {
	args := map[string]string{}
	for len(a) >= 2 {
		key := a[0]
		if !strings.HasPrefix(key, "-") || len(key) < 2 {
			fatal("error: unexpected argument %s\n", key)
		}
		args[key[1:]] = a[1]
		a = a[2:]
	}
	if len(a) == 1 {
		args[query.DefaultArgKey] = a[0]
	}
	return args
}

type filterList []string

func (f *filterList) String() string { return strings.Join(*f, ",") }
func (f *filterList) Set(s string) error {
	*f = append(*f, s)
	return nil
}

func fatal(f string, v ...any) {
	fmt.Fprintf(os.Stderr, f, v...)
	os.Exit(1)
}
