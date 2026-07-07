package log

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opListenArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpListen(ctx *astral.Context, q *routing.IncomingQuery, args opListenArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	fwd := &logForwarder{ch}

	mod.log.AddLogger(fwd)
	defer mod.log.RemoveLogger(fwd)

	for {
		_, err := ch.Receive()
		if err != nil {
			return nil
		}
	}
}

type logForwarder struct {
	ch channel.Sender
}

func (l logForwarder) LogEntry(entry *log.Entry) {
	l.ch.Send(entry)
}
