package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	objectsClient "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// provide is the provider app on node2. It prints one JSON line of facts once
// the node holds its advertisement, then serves until stdin closes.
func provide(path string) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	n2 := nodes["node2"]
	ctx := astral.NewContext(nil)

	if err := registerTypes(); err != nil {
		return err
	}
	a, err := register(ctx, n2, serveApps)
	if err != nil {
		return err
	}
	// why: apps.SyncBlueprints talks to the process-wide default client.
	astrald.SetDefault(a.client)

	ops := routing.NewOpRouter()
	op, err := routing.NewOp(func(_ *astral.Context, q *routing.IncomingQuery, _ struct{}) error {
		ch := q.Accept()
		defer ch.Close()
		return ch.Send(&Reading{Label: readingLabel, Serial: readingSerial})
	})
	if err != nil {
		return err
	}
	if err := ops.AddOp(opName, op); err != nil {
		return err
	}

	p := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		service: func(*astral.Context, *astral.Identity) (*services.Update, error) {
			info := astral.NewBundle()
			if err := info.Append(&Offer{Label: offerLabel, Tier: offerTier}); err != nil {
				return nil, err
			}
			return &services.Update{Available: true, Info: info}, nil
		},
	})

	var once sync.Once
	facts := map[string]any{}
	ready := make(chan struct{})
	// The hooks run in the order given: sync, a look at what the node holds,
	// then the advertisement.
	look := func(hctx *astral.Context) error {
		names, err := objectsClient.New(nil, a.client).Blueprints(hctx)
		if err != nil {
			return err
		}
		held := map[string]bool{}
		for _, n := range names {
			held[n] = true
		}
		once.Do(func() { facts["types_held_before_advertise"] = held[offerType] && held[readingType] })
		return nil
	}
	advertised := func(hctx *astral.Context) error {
		if err := p.Serve(hctx, servicescli.New(nil, a.client)); err != nil {
			return err
		}
		select {
		case <-ready:
		default:
			close(ready)
		}
		return nil
	}

	sctx, cancel := ctx.WithCancel()
	defer cancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, a.client)))
	served := make(chan error, 1)
	go func() {
		served <- apps.ServeWith(sctx, ops, reg,
			apps.WithBlueprintSync(), apps.WithRegistrationHook(look), apps.WithRegistrationHook(advertised))
	}()

	select {
	case <-ready:
	case err := <-served:
		return fmt.Errorf("serving: %v", err)
	}
	facts["provider_identity"] = a.id.String()
	out, _ := json.Marshal(facts)
	fmt.Println(string(out))

	_, _ = io.Copy(io.Discard, os.Stdin)
	cancel()
	_ = p.Close()
	<-served
	return nil
}

var _ = channel.Expect[*astral.Ack]
