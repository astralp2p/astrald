// Command driver calls an app identity no node hosts, twice, from an app on
// node1, and times each answer. node1 asks its linked siblings for the
// identity's relay contracts on the first miss only.
//
//	driver drive <session.json>   prints facts as JSON
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

// call is one call's outcome.
type call struct {
	Ms            int64  `json:"ms"`
	RouteNotFound bool   `json:"route_not_found"`
	Err           string `json:"err"`
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "drive" {
		fmt.Fprintln(os.Stderr, "usage: driver drive <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	out, _ := json.Marshal(map[string]any{"relay_pull_absent": facts})
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]any, error) {
	facts := map[string]any{}
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
	n1 := s.Nodes["node1"]
	ctx := astral.NewContext(nil)

	caller, err := apphostcli.New(nil, connect(n1.Endpoint, n1.Token)).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the caller on node1: %w", err)
	}
	callerCli := connect(n1.Endpoint, string(caller.Token))

	// an app identity no node hosts and no node holds a contract of
	target := astral.GenerateIdentity()
	facts["target"] = target.String()

	var calls []call
	for range 2 {
		calls = append(calls, ping(ctx, callerCli, target))
	}
	facts["calls"] = calls

	fmt.Fprintf(os.Stderr, "calls to an absent app: %+v\n", calls)
	return facts, nil
}

// ping queries target:ping as the client's app and times the answer.
func ping(ctx *astral.Context, caller *astrald.Client, target *astral.Identity) call {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()

	start := time.Now()
	ch, err := caller.WithTarget(target).QueryChannel(cctx, "ping", nil)
	if err == nil {
		err = ch.Switch(channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF())
		ch.Close()
	}
	c := call{Ms: time.Since(start).Milliseconds()}
	if err == nil {
		err = errors.New("the absent app answered")
	}
	c.Err = err.Error()
	// note: the node answers route_not_found over apphost; the router's own
	// error reads "route not found"
	text := strings.ReplaceAll(strings.ToLower(err.Error()), "_", " ")
	c.RouteNotFound = errors.Is(err, astral.NewErrRouteNotFound()) || strings.Contains(text, "route not found")
	return c
}
