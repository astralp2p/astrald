package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	dircli "github.com/astralp2p/astral-go/api/dir/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/lib/arl"
)

const (
	EnvDefaultTarget = "ASTRAL_DEFAULT_TARGET"
)

func main() {
	var zoneFlag string
	var filterFlag filterList
	var eosFlag bool

	flag.StringVar(&zoneFlag, "zone", "", "zones to include: any combination of d(evice), v(irtual), n(etwork)")
	flag.Var(&filterFlag, "filter", "identity `filter` to apply (repeatable)")
	flag.BoolVar(&eosFlag, "eos", false, "after input ends, send an EOS so a streaming op (objects.store and the like) returns instead of waiting for more input")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-zone dvn] [-filter name]... [-eos] <query> [-arg <val>]...\n", os.Args[0])
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
	inFmt := args["in"]
	go func() {
		io.Copy(conn, os.Stdin)

		// why: a streaming op reads its input channel until an EOS and otherwise holds the
		// session open; io.Copy ending on stdin EOF sends no such marker, so a batch op
		// (objects.store and the like) never returns and the caller waits on it forever. -eos
		// signals end-of-input so the op completes. It is opt-in because an interactive or
		// long-lived session must not have its input closed under it; a caller that pipes a
		// bounded input and wants the op to finish asks for it. Nothing to terminate when no
		// input format was set, so it is skipped even under the flag.
		if eosFlag && inFmt != "" {
			_ = sendEndOfInput(conn, inFmt)
		}
	}()

	io.Copy(os.Stdout, conn)
}

// sendEndOfInput writes an EOS marker to rw, encoded in format — the format the op decodes its
// input channel with, since rw carries what the op reads. It is what tells a streaming op that
// no more input follows.
func sendEndOfInput(rw io.ReadWriter, format string) error {
	return channel.New(rw, channel.WithFormats("", format)).Send(&astral.EOS{})
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
