package main

import (
	"fmt"
	"time"

	"encoding/json"
	"os"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// node is one node of the session view.
type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

func loadNodes(path string) (map[string]node, error) {
	nodes, _, err := loadSession(path)
	return nodes, err
}

// loadSession reads the nodes of the session view and the User's token.
func loadSession(path string) (map[string]node, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
		Facts struct {
			UserToken string `json:"user_token"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, "", err
	}
	return s.Nodes, s.Facts.UserToken, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// app is a registered app identity with a client of its own.
type app struct {
	id     *astral.Identity
	client *astrald.Client
	n      node
}

// register mints a fresh app identity on n through the node's admin token.
func register(ctx *astral.Context, n node, permits ...*auth.Permit) (*app, error) {
	tok, err := apphostcli.New(nil, connect(n.Endpoint, n.Token)).Register(ctx, permits...)
	if err != nil {
		return nil, err
	}
	return &app{id: tok.Identity, client: connect(n.Endpoint, string(tok.Token)), n: n}, nil
}

// discovery is a one-shot discovery read to its end.
type discovery struct {
	providers []*astral.Identity
	updates   []*services.Update
	outcome   *servicescli.InitialOutcome
	err       error
}

func discoverOnce(ctx *astral.Context, a *app, reach services.Reach, service string) discovery {
	events, err := servicescli.New(nil, a.client).DiscoverIn(ctx, reach, []string{service}, false)
	if err != nil {
		return discovery{err: err}
	}
	var d discovery
	for ev := range events {
		switch {
		case ev.Update != nil && bool(ev.Update.Available):
			d.providers = append(d.providers, ev.Update.ProviderID)
			d.updates = append(d.updates, ev.Update)
		case ev.Initial != nil:
			d.outcome = ev.Initial
		case ev.Err != nil:
			d.err = ev.Err
		}
	}
	return d
}

func (d discovery) complete() bool { return d.err == nil && d.outcome != nil && d.outcome.Complete }

// awaitHost waits until apphost.hosts on admin's node names host among the
// hosts of app.
func awaitHost(ctx *astral.Context, admin *astrald.Client, app, host *astral.Identity) ([]*astral.Identity, error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		out, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		var hosts []*astral.Identity
		for h := range out {
			hosts = append(hosts, h)
		}
		for _, h := range hosts {
			if h.IsEqual(host) {
				return hosts, nil
			}
		}
		if time.Now().After(deadline) {
			return hosts, fmt.Errorf("apphost.hosts named %v (err %v) for %v; want %v", hosts, *errp, app, host)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
