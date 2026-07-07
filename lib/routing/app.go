package routing

import (
	"fmt"
	"io"
	"os"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// App is a self-describing ScopeRouter that automatically exposes a ".spec"
// op listing all registered operations.
type App struct {
	*ScopeRouter
}

type SpecArgs struct {
	In  string
	Out string
}

// NewApp wraps s in an OpRouter, mounts it in a ScopeRouter, and injects a
// ".spec" op that streams the full operation manifest to callers.
func NewApp(s any) *App {
	ops := NewOpRouter()
	ops.AddStruct(s)

	app := App{ScopeRouter: NewScopeRouter(ops)}

	spec, _ := NewOp(app.Spec)
	ops.AddOp(".spec", spec)
	return &app
}

// Spec streams all OpSpec entries for the app and terminates with an EOS object.
func (app *App) Spec(_ *astral.Context, query *IncomingQuery, args SpecArgs) error {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	for _, opSpec := range app.ScopeRouter.Spec() {
		err := ch.Send(&opSpec)
		if err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

func (app *App) Add(scope string, s any) {
	app.ScopeRouter.Add(scope, NewOpRouter(s))
}

// Run routes args[0] as an op name (with remaining args as query params) against
// the app and bridges the resulting connection to stdin/stdout.
func (app *App) Run(ctx *astral.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing command")
	}

	q := query.New(ctx.Identity(), ctx.Identity(), args[0], query.ArgsToMap(args[1:]))
	conn, err := query.RouteInFlight(ctx, app, astral.Launch(q))
	if err != nil {
		return err
	}

	go func() {
		io.Copy(conn, os.Stdin)
		conn.Close()
	}()

	_, err = io.Copy(os.Stdout, conn)
	return err
}
