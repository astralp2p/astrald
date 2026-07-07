package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	fmt2 "github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	_ "github.com/cryptopunkscc/astrald/mod/all/pub"
	_ "github.com/cryptopunkscc/astrald/mod/all/views"
	"github.com/cryptopunkscc/astrald/mod/log/views"
)

func main() {
	views.UseIdentityView()

	var err error
	var replay bool
	var raw bool
	var i io.Reader = os.Stdin
	filter := &EntryFilter{}

	flag.Usage = func() {
		fmt.Printf("usage: astral-logview [options] [file]\n\n")
		fmt.Printf("Default file is stdin.\n\n")
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
	}

	flag.StringVar(&filter.Tag, "tag", "", "show only entries with tag")
	flag.UintVar(&filter.Level, "level", 3, "max level to show")
	flag.BoolVar(&replay, "replay", false, "replay log in real time")
	flag.BoolVar(&raw, "raw", false, "don't load aliases")
	flag.Parse()

	args := flag.Args()

	var ctx = astrald.NewContext()

	if !raw {
		err = loadAliasMap(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading alias map: %v\n", err)
		}
	}

	if len(args) > 0 && args[0] != "-" {
		i, err = os.Open(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)
			os.Exit(1)
		}
	}

	var ch = channel.NewBinaryReceiver(i)

	var lastTime astral.Time

	for {
		o, err := ch.Receive()
		switch entry := o.(type) {
		case *log.Entry:
			if replay {
				if !lastTime.Time().IsZero() {
					time.Sleep(entry.Time.Time().Sub(lastTime.Time()))
				}
				lastTime = entry.Time
			}

			if !filter.Filter(entry) {
				continue
			}

			fmt2.Println(entry)

		case *astral.EOS:
			return

		case nil:
			if errors.Is(err, io.EOF) {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "receive error: %v\n", err)
			os.Exit(1)

		default:
			fmt.Fprintf(os.Stderr, "unexpected object type %s (%T)\n", entry.ObjectType(), entry)
			os.Exit(2)
		}

	}
}
