// Command driver checks that an app discovered on another swarm node can be
// called by its app identity, with no relink, once apphost.register has pushed
// its relay contract to the sibling.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same round trip with fresh identities
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"services_app_call": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]bool, error) {
	facts := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	ctx := astral.NewContext(nil)
	admin1 := connect(n1.Endpoint, n1.Token)

	// the provider app on node2, serving one stand-in player op
	provider, err := apphostcli.New(nil, connect(n2.Endpoint, n2.Token)).Register(ctx, "mod.auth.serve_apps_action")
	if err != nil {
		return facts, err
	}
	stop, err := serveProvider(ctx, n2.Endpoint, string(provider.Token))
	if err != nil {
		return facts, err
	}
	defer stop()

	// the front-end app on node1, granted player discovery on any node
	front, err := apphostcli.New(nil, admin1).Register(ctx)
	if err != nil {
		return facts, err
	}
	if err := grantDiscovery(ctx, admin1, front.Identity); err != nil {
		return facts, err
	}
	frontCli := connect(n1.Endpoint, string(front.Token))

	found, err := discovered(ctx, frontCli, provider.Identity)
	facts["discovered_on_node2"] = found
	if err != nil || !found {
		return facts, fmt.Errorf("swarm discovery did not show the provider: %v", err)
	}

	// note: the provider registered after the link came up, so only the push
	// apphost.register makes carries its relay contract to node1.
	var seen string
	deadline := time.Now().Add(15 * time.Second)
	for {
		seen, err = call(ctx, frontCli, provider.Identity, 10*time.Second)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	facts["called_without_relink"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("calling the provider registered after the link: %w", err)
	}
	facts["provider_saw_the_app"] = seen == front.Identity.String()
	if seen != front.Identity.String() {
		return facts, fmt.Errorf("the provider saw caller %v; want the app %v", seen, front.Identity)
	}
	fmt.Fprintln(os.Stderr, "ok: provider app on node2 called by its identity from an app on node1")
	return facts, nil
}

// serveProvider runs an app on node2 that advertises player and answers
// player.state with the caller it saw.
func serveProvider(ctx *astral.Context, endpoint, token string) (func(), error) {
	router := apphost.NewRouter(endpoint, token)
	apphost.SetDefaultRouter(router)
	astrald.SetDefault(astrald.New(router))

	ops := routing.NewOpRouter()
	op, err := routing.NewOp(func(_ *astral.Context, q *routing.IncomingQuery) error {
		ch := q.Accept()
		defer ch.Close()
		return ch.Send(astral.NewString8(q.Caller().String()))
	})
	if err != nil {
		return nil, err
	}
	if err := ops.AddOp("player.state", op); err != nil {
		return nil, err
	}
	p := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		"player": func(*astral.Context, *astral.Identity) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		},
	})
	sctx, cancel := ctx.WithCancel()
	go apps.Serve(sctx, ops, apps.WithServices(p))
	return cancel, nil
}

func grantDiscovery(ctx *astral.Context, admin *astrald.Client, app *astral.Identity) error {
	ch, err := admin.QueryChannel(ctx, "apphost.grant", query.Args{
		"identity": app.String(), "action": services.ServiceDiscoveryAction{}.ObjectType(), "constrained": true,
	})
	if err != nil {
		return err
	}
	defer ch.Close()
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{"player"}}}}
	if err := cs.Append(scope); err != nil {
		return err
	}
	if err := ch.Send(cs); err != nil {
		return err
	}
	return ch.Switch(channel.ExpectAck, channel.PassErrors)
}

// discovered waits until a swarm discovery from the front-end shows provider.
func discovered(ctx *astral.Context, front *astrald.Client, provider *astral.Identity) (bool, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := servicescli.New(nil, front).DiscoverIn(ctx, services.ReachSwarm, []string{"player"}, false)
		if err != nil {
			return false, err
		}
		for ev := range events {
			if ev.Update != nil && ev.Update.ProviderID.IsEqual(provider) {
				return true, nil
			}
		}
		time.Sleep(time.Second)
	}
	return false, nil
}

// call queries provider:player.state as the front-end and returns the caller
// the provider reported.
func call(ctx *astral.Context, front *astrald.Client, provider *astral.Identity, timeout time.Duration) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(timeout)
	defer cancel()
	ch, err := front.WithTarget(provider).QueryChannel(cctx, "player.state", nil)
	if err != nil {
		return "", err
	}
	defer ch.Close()
	var seen *astral.String8
	if err := ch.Switch(channel.Expect(&seen), channel.PassErrors, channel.WithContext(cctx)); err != nil {
		return "", err
	}
	if seen == nil {
		return "", errors.New("no answer")
	}
	return seen.String(), nil
}
