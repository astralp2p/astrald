package main

import (
	"fmt"
	"sync"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

const (
	opDecide    = "control.decide_app_register"
	opAuthorize = "control.authorize"
	opPing      = "control.ping"
)

// controlPlane is the app the node delegates to. It serves the registration
// decision, an authorizer, and a ping.
type controlPlane struct {
	id           *astral.Identity
	nodeEndpoint string

	mu          sync.Mutex
	hold        time.Duration
	authorizers []*auth.AuthorizerAssignment

	received chan *apphostapi.AppRegisterRequest
	ended    chan string
	asked    chan string
}

// set chooses how long the next decisions wait and which authorizers they name.
func (cp *controlPlane) set(hold time.Duration, authorizers []*auth.AuthorizerAssignment) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.hold, cp.authorizers = hold, authorizers
}

func (cp *controlPlane) endpoint() string {
	return "astral://" + cp.id.String() + ":" + opAuthorize
}

func (cp *controlPlane) decide(ctx *astral.Context, q *routing.IncomingQuery) error {
	conn := q.AcceptRaw()
	ch := channel.New(conn)
	defer ch.Close()

	var req *apphostapi.AppRegisterRequest
	if err := ch.Switch(channel.Expect(&req), channel.PassErrors); err != nil {
		return err
	}
	cp.received <- req

	// why: the node hangs up when the registering client leaves or the
	// question's bound ends, and the wait here must end with it.
	wctx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	cp.mu.Lock()
	hold, authorizers := cp.hold, cp.authorizers
	cp.mu.Unlock()

	started := time.Now()
	select {
	case <-time.After(hold):
		cp.ended <- "held " + hold.String()
	case <-wctx.Done():
		cp.ended <- "node hung up after " + time.Since(started).Round(100*time.Millisecond).String()
		return nil
	}
	return ch.Send(&apphostapi.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    req.GrantPermits,
		ContractPermits: req.ContractPermits,
		Authorizers:     authorizers,
	})
}

// authorize records the action it is asked about and allows it.
func (cp *controlPlane) authorize(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	obj, err := ch.Receive()
	if err != nil {
		return err
	}
	desc := obj.ObjectType()
	if a, ok := obj.(auth.ActionObject); ok {
		desc += " actor=" + a.Actor().String()
	}
	cp.asked <- desc
	return ch.Send(&astral.Ack{})
}

func (cp *controlPlane) ping(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(&astral.Ack{})
}

// startControlPlane registers the app, serves its ops until ctx ends, and
// returns once the node routes to it.
func startControlPlane(ctx *astral.Context, admin *astrald.Client, ep string) (*controlPlane, error) {
	tok, err := apphostcli.New(nil, admin).Register(ctx, "mod.auth.serve_apps_action")
	if err != nil {
		return nil, fmt.Errorf("register the control plane: %w", err)
	}
	cp := &controlPlane{
		id:           tok.Identity,
		nodeEndpoint: ep,
		received:     make(chan *apphostapi.AppRegisterRequest, 8),
		ended:        make(chan string, 8),
		asked:        make(chan string, 8),
	}

	router := apphost.NewRouter(ep, string(tok.Token))
	apphost.SetDefaultRouter(router)
	astrald.SetDefault(astrald.New(router))

	ops := routing.NewOpRouter()
	for name, fn := range map[string]any{opDecide: cp.decide, opAuthorize: cp.authorize, opPing: cp.ping} {
		op, err := routing.NewOp(fn)
		if err != nil {
			return nil, err
		}
		if err := ops.AddOp(name, op); err != nil {
			return nil, err
		}
	}
	go apps.Serve(ctx, ops)
	return cp, awaitRoutable(ctx, admin, cp.id)
}

func awaitRoutable(ctx *astral.Context, admin *astrald.Client, id *astral.Identity) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := ack(admin.WithTarget(id).QueryChannel(ctx, opPing, nil))
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the control plane never became routable: %w", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
