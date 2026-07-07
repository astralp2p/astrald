package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

const DefaultTokenDuration = astral.Duration(time.Hour * 24 * 365)       // 1 year
const DefaultAppContractDuration = astral.Duration(time.Hour * 24 * 365) // 1 year

type opCreateTokenArgs struct {
	ID       *astral.Identity
	Duration astral.Duration `query:"optional"`
	Out      string          `query:"optional"`
}

func (mod *Module) OpCreateToken(ctx *astral.Context, q *routing.IncomingQuery, args opCreateTokenArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.ID.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if args.Duration == 0 {
		args.Duration = DefaultTokenDuration
	}

	mod.log.Logv(1, "creating token for %v valid for %v", args.ID, args.Duration)

	token, err := mod.CreateAccessToken(args.ID, args.Duration)
	if err != nil {
		mod.log.Errorv(1, "error creating token for %v: %v", args.ID, err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	return ch.Send(token)
}
