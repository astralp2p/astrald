package shell

import (
	"errors"
	authmod "github.com/cryptopunkscc/astrald/mod/auth"
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opShellArgs struct {
	As astral.String8 `query:"optional"`
}

func (mod *Module) OpShell(ctx *astral.Context, q *routing.IncomingQuery, args opShellArgs) (err error) {
	// handle args
	if len(args.As) > 0 {
		asID, err := mod.Dir.ResolveIdentity(string(args.As))
		if err != nil {
			return err
		}

		if !mod.Auth.Authorize(ctx, &authmod.SudoAction{Action: auth.NewAction(q.Caller()), AsID: asID}) {
			return astral.NewError("access denied")
		}

		ctx = ctx.WithIdentity(asID)
	} else {
		ctx = ctx.WithIdentity(q.Caller())
	}

	// accept
	var conn io.ReadWriteCloser
	conn = q.AcceptRaw()
	defer conn.Close()

	// handle session
	err = NewSession(mod, conn).Run(ctx)
	switch {
	case err == nil, errors.Is(err, io.EOF):
		mod.log.Logv(1, "shell session with %v ended", ctx.Identity())
		err = nil
	default:
		mod.log.Errorv(1, "shell session with %v ended in error: %v", ctx.Identity(), err)
	}

	return
}
