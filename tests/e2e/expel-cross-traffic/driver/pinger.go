package main

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// pinger is an app that answers probe.ping with "pong" and counts the queries
// it served.
type pinger struct {
	id     *astral.Identity
	served atomic.Int64
}

type pingArgs struct {
	Out string
}

// Ping answers "pong".
func (p *pinger) Ping(_ *astral.Context, q *routing.IncomingQuery, args pingArgs) error {
	p.served.Add(1)
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	return ch.Send(astral.NewString8("pong"))
}

// servePinger registers a fresh app on n and serves it on a handler at n's
// host_ip until the driver ends.
func (w *world) servePinger(n provider.Node) (*pinger, error) {
	if n.HostIP == "" {
		return nil, fmt.Errorf("node %v has no host_ip", n.Identity)
	}
	app, err := provider.Register(w.ctx, n.Admin(), n, serveApps)
	if err != nil {
		return nil, err
	}
	p := &pinger{id: app.ID}
	router := routing.NewApp(&struct{}{})
	router.Add("probe", p)

	ready := make(chan struct{})
	var once sync.Once
	hook := func(*astral.Context) error {
		once.Do(func() { close(ready) })
		return nil
	}
	ctx, cancel := w.ctx.WithCancel()
	w.stops = append(w.stops, cancel)
	reg := apps.NewAppRegistrar(ctx, apps.WithClient(apphostcli.New(nil, app.Client)))
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(n.HostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(ctx, router, reg, at, apps.WithRegistrationHooks(hook)) }()

	select {
	case <-ready:
		return p, nil
	case err := <-served:
		return nil, fmt.Errorf("serving app %v: %v", app.ID, err)
	case <-time.After(readyWithin):
		return nil, fmt.Errorf("serving app %v: not registered within %v", app.ID, readyWithin)
	}
}

// pingOnce asks the target of client for probe.ping and reads one "pong".
func pingOnce(ctx *astral.Context, client *astrald.Client) error {
	ch, err := client.QueryChannel(ctx, "probe.ping", nil)
	if err != nil {
		return err
	}
	defer ch.Close()
	obj, err := ch.Receive()
	if err != nil {
		return err
	}
	if s, ok := obj.(*astral.String8); !ok || string(*s) != "pong" {
		return fmt.Errorf("answered %v; want pong", obj)
	}
	return nil
}
