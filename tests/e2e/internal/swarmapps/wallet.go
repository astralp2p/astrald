package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// Limit is the largest amount the wallet spends in one call. The wallet's own
// handler holds it, whatever the caller's contracts allow.
const Limit = 100

type spendArgs struct {
	Amount int64
}

// serveWallet runs the wallet stand-in on the node at ep as the app tok names:
// it advertises Service as private, so only a caller holding a discovery
// permit sees it, and answers wallet.spend up to Limit. It writes its state
// to $ASTRAL_TESTS_APP_STATE once the handler is attached.
func serveWallet(ctx *astral.Context, ep, tok, hostIP string) error {
	router := apphost.NewRouter(ep, tok)
	apphost.SetDefaultRouter(router)
	astrald.SetDefault(astrald.New(router))

	ops := routing.NewOpRouter()
	op, err := routing.NewOp(func(_ *astral.Context, q *routing.IncomingQuery, args spendArgs) error {
		ch := q.Accept()
		defer ch.Close()
		if args.Amount > Limit {
			return ch.Send(astral.NewError(fmt.Sprintf("over limit %d", Limit)))
		}
		return ch.Send(astral.NewString8(fmt.Sprintf("spent %d for %v", args.Amount, q.Caller())))
	})
	if err != nil {
		return err
	}
	if err = ops.AddOp(Service+".spend", op); err != nil {
		return err
	}

	p := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		Service: func(*astral.Context, *astral.Identity) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		},
	})
	p.SetOpen(false)

	// note: the state only says the process serves; the driver waits for
	// discovery to show the wallet itself.
	writeState(map[string]any{"serving": true})
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(hostIP, "0"))
	return apps.Serve(ctx, ops, at, apps.WithServices(p))
}

func writeState(v map[string]any) {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if path == "" {
		return
	}
	b, _ := json.Marshal(v)
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// discover runs one discovery of Service at reach as the app c's token names
// and returns the providers it showed.
func discover(ctx *astral.Context, c *astrald.Client, reach string) (map[string]any, error) {
	r, err := services.ParseReach(reach)
	if err != nil {
		return nil, err
	}
	dctx, cancel := ctx.WithTimeout(10 * time.Second)
	defer cancel()
	events, err := servicescli.New(nil, c).DiscoverIn(dctx, r, []string{Service}, false)
	if err != nil {
		return nil, err
	}
	providers := []string{}
	for ev := range events {
		if ev.Update != nil && ev.Update.Available && ev.Update.ProviderID != nil {
			providers = append(providers, ev.Update.ProviderID.String())
		}
	}
	return map[string]any{"providers": providers}, nil
}

// call queries app:wallet.spend as the app c's token names.
func call(ctx *astral.Context, c *astrald.Client, app string, amount int64) (map[string]any, error) {
	target, err := astral.ParseIdentity(app)
	if err != nil {
		return nil, err
	}
	cctx, cancel := ctx.WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := c.WithTarget(target).QueryChannel(cctx, Service+".spend", map[string]string{"amount": strconv.FormatInt(amount, 10)})
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	var answer *astral.String8
	err = ch.Switch(channel.Expect(&answer), channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF())
	if err != nil {
		return nil, err
	}
	if answer == nil {
		return nil, errors.New("no answer")
	}
	return map[string]any{"answer": answer.String()}, nil
}
