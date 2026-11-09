package shell

import (
	"context"
	"io"

	shell2 "bitbucket.org/creachadair/shell"
	"github.com/astralp2p/astral-go/api/shell"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/streams"
	shellmod "github.com/astralp2p/astrald/mod/shell"
)

type Session struct {
	mod    *Module
	rwc    io.ReadWriteCloser
	cr     *streams.ContextReader
	origin string
}

// NewSession returns a session over conn. origin is the origin of the query
// that opened the session and is carried by every command it runs; empty means
// local.
func NewSession(mod *Module, conn io.ReadWriteCloser, origin string) *Session {
	cr := streams.NewContextReader(conn)
	rwc := streams.ReadWriteCloseSplit{
		Reader: cr.WithContext(context.Background()),
		Writer: conn,
		Closer: conn,
	}

	return &Session{
		mod:    mod,
		rwc:    rwc,
		cr:     cr,
		origin: origin,
	}
}

func (s *Session) Run(ctx *astral.Context) (err error) {
	var t = shellmod.NewTerminal(s.rwc)

	for {
		// print the prompt
		t.Print(&shell.Prompt{
			GuestID: ctx.Identity(),
			HostID:  s.mod.node.Identity(),
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

		if args[0] == "exit" {
			return nil
		}

		conn, err := query.RouteInFlight(ctx, s.mod.node, s.launchCommand(ctx.Identity(), args))
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

// launchCommand returns the query for a command line of the session, made by
// caller.
func (s *Session) launchCommand(caller *astral.Identity, args []string) *astral.InFlightQuery {
	q := astral.Launch(query.New(caller, s.mod.node.Identity(), args[0], query.ArgsToMap(args[1:])))

	q.Extra.Set("interface", "terminal")

	// why: a command of a session opened over a link must stay a network query,
	// so ops that refuse network callers refuse it too. The value is a string:
	// routing.Op reads it with o.(string).
	if s.origin != "" {
		q.Extra.Set("origin", s.origin)
	}

	return q
}
