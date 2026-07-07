package shell

import (
	"context"
	"io"

	shell2 "bitbucket.org/creachadair/shell"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/streams"
	"github.com/cryptopunkscc/astral-go/lib/query"
	"github.com/cryptopunkscc/astrald/mod/shell"
)

type Session struct {
	mod *Module
	rwc io.ReadWriteCloser
	cr  *streams.ContextReader
}

func NewSession(mod *Module, conn io.ReadWriteCloser) *Session {
	cr := streams.NewContextReader(conn)
	rwc := streams.ReadWriteCloseSplit{
		Reader: cr.WithContext(context.Background()),
		Writer: conn,
		Closer: conn,
	}

	return &Session{
		mod: mod,
		rwc: rwc,
		cr:  cr,
	}
}

func (s *Session) Run(ctx *astral.Context) (err error) {
	var t = shell.NewTerminal(s.rwc)

	for {
		// print the prompt
		t.Print(&Prompt{
			guestID: ctx.Identity(),
			hostID:  s.mod.node.Identity(),
		})

		// read the command
		var line string
		line, err = t.ReadLine()
		if err != nil {
			return err
		}

		args, valid := shell2.Split(line)
		switch {
		case !valid:
			t.Printf("quote mismatch\n")
			continue

		case len(args) == 0:
			continue
		}

		op := args[0]
		if op == "exit" {
			return nil
		}

		params := query.ArgsToMap(args[1:])

		var q = astral.Launch(query.New(ctx.Identity(), s.mod.node.Identity(), op, params))

		q.Extra.Set("interface", "terminal")

		conn, err := query.RouteInFlight(ctx, s.mod.node, q)
		if err != nil {
			t.Printf("error: %v\n", err)
			continue
		}

		opCtx, cancelOp := context.WithCancel(ctx)
		r := s.cr.WithContext(opCtx)

		go io.Copy(conn, r)

		io.Copy(s.rwc, conn)
		conn.Close()
		cancelOp()

		t.Printf("ok\n")
	}
}
