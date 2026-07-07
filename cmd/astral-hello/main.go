package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/apps"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	objectsClient "github.com/cryptopunkscc/astrald/mod/objects/client"
)

type App struct{}

type HelloArgs struct {
	Name string `query:"required"`
	Out  string
}

func (app *App) Hello(ctx *astral.Context, q *routing.IncomingQuery, args HelloArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	return ch.Send(&Greeting{
		Recipient: astral.String8(args.Name),
		Message:   astral.String16("hello, " + args.Name + "!"),
	})
}

// verifyGreeting round-trips a Greeting through the node's strict objects.echo. The node
// must decode the wire bytes via the registered blueprint and re-encode from the parsed
// value, so a successful round-trip with matching fields proves the node has the schema —
// not just the type name.
func verifyGreeting(ctx *astral.Context) error {
	ch, err := objectsClient.Echo(ctx, objectsClient.EchoOptions{Strict: true})
	if err != nil {
		return fmt.Errorf("verify: open echo: %w", err)
	}
	defer ch.Close()

	sent := &Greeting{
		Recipient: astral.String8("astrald"),
		Message:   astral.String16("hello astral!"),
	}
	if err := ch.Send(sent); err != nil {
		return fmt.Errorf("verify: send: %w", err)
	}

	var (
		got     astral.Object
		echoErr error
	)
	err = ch.Switch(
		func(e *astral.ErrorMessage) error {
			echoErr = fmt.Errorf("echo rejected: %s", e.Error())
			return channel.ErrBreak
		},
		func(o astral.Object) error {
			got = o
			return channel.ErrBreak
		},
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("verify: receive: %w", err)
	}
	if echoErr != nil {
		return fmt.Errorf("verify: %w", echoErr)
	}

	g, ok := got.(*Greeting)
	if !ok {
		return fmt.Errorf("verify: got %T, want *Greeting", got)
	}
	if g.Recipient != sent.Recipient || g.Message != sent.Message {
		return fmt.Errorf("verify: data mismatch: got %+v, want %+v", g, sent)
	}

	fmt.Println("astrald greeting verification successfully")
	return nil
}

func main() {
	ctx, cancel := astrald.NewContext().WithCancelCause()
	defer cancel(nil)

	// Closed by a sentinel registration hook after all prior hooks (e.g. WithBlueprintSync)
	// have run. Keep the sentinel last so it observes the completion of every hook before it.
	ready := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)

	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = apps.Serve(ctx,
			routing.NewApp(&App{}),
			apps.WithBlueprintSync(),
			apps.WithRegistrationHook(func(*astral.Context) error {
				close(ready)
				return nil
			}),
		)
	}()

	select {
	case <-ready:
		err := verifyGreeting(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			cancel(fmt.Errorf("verify failed: %w", err))
			return
		}

	case <-time.After(10 * time.Second):
		fmt.Fprintln(os.Stderr, "startup timeout: hooks didn't complete in 10s")
		cancel(fmt.Errorf("startup timeout"))
	}

	wg.Wait()
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) {
		fmt.Fprintln(os.Stderr, serveErr)
	}
}
