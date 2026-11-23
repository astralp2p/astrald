// Command meshapp is the long-lived mesh app of the swarm-comms tests: it
// serves hello, advertises a service, follows it with reach=swarm and calls
// every provider the follow reports.
//
//	meshapp serve -service S [-period 1s] [-keep-removed] [-targets id,...]
//	              [-probe-nodes id,...] [-probe-op apphost.whoami] [-call-timeout 65s]
//	meshapp call -target ID [-op hello] [-as-app] [-timeout 15s]
//
// Both read the node from APP_NODE_ENDPOINT and APP_NODE_TOKEN (the node's
// admin token); serve binds its handler at APP_HOST_IP and writes its state to
// ASTRAL_TESTS_APP_STATE. tests/lib/meshapps.py starts it; README.md holds the
// state file's fields.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: meshapp serve | call")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "call":
		err = runCall(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// env is what the harness hands the app through its environment.
type env struct {
	endpoint  string
	token     string
	hostIP    string
	statePath string
}

// readEnv reads the app's environment; serving needs the host address and
// the state file too.
func readEnv(serving bool) (env, error) {
	e := env{endpoint: os.Getenv("APP_NODE_ENDPOINT"), token: os.Getenv("APP_NODE_TOKEN"),
		hostIP: os.Getenv("APP_HOST_IP"), statePath: os.Getenv("ASTRAL_TESTS_APP_STATE")}
	required := map[string]string{"APP_NODE_ENDPOINT": e.endpoint, "APP_NODE_TOKEN": e.token}
	if serving {
		required["APP_HOST_IP"], required["ASTRAL_TESTS_APP_STATE"] = e.hostIP, e.statePath
	}
	for name, v := range required {
		if v == "" {
			return env{}, fmt.Errorf("%v is not set", name)
		}
	}
	return e, nil
}

// untilSignal is ctx until SIGTERM or SIGINT.
func untilSignal(ctx *astral.Context) (*astral.Context, func()) {
	sctx, cancel := ctx.WithCancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigs:
			cancel()
		case <-sctx.Done():
		}
	}()
	return sctx, cancel
}

// parseIdentities parses a comma-separated identity list; empty is none.
func parseIdentities(list string) ([]*astral.Identity, error) {
	var out []*astral.Identity
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := astral.ParseIdentity(s)
		if err != nil {
			return nil, fmt.Errorf("identity %q: %w", s, err)
		}
		out = append(out, id)
	}
	return out, nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var f serveFlags
	var targets, probes string
	fs.StringVar(&f.service, "service", "", "the service the app advertises and follows")
	fs.DurationVar(&f.period, "period", time.Second, "the time between two calls to one peer; 0 makes one call")
	fs.BoolVar(&f.keepRemoved, "keep-removed", false, "keep calling a provider the follow removed")
	fs.StringVar(&targets, "targets", "", "app identities called as if reported")
	fs.StringVar(&probes, "probe-nodes", "", "node identities queried with -probe-op")
	fs.StringVar(&f.probeOp, "probe-op", "apphost.whoami", "the op of a node probe")
	fs.DurationVar(&f.callTimeout, "call-timeout", 65*time.Second, "the deadline of one call")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.service == "" {
		return errors.New("serve needs -service")
	}
	var err error
	if f.targets, err = parseIdentities(targets); err != nil {
		return err
	}
	if f.probeNodes, err = parseIdentities(probes); err != nil {
		return err
	}
	return serve(f)
}

// callResult is the one line meshapp call prints.
type callResult struct {
	Caller  string  `json:"caller"`
	Target  string  `json:"target"`
	Outcome string  `json:"outcome"`
	Code    int     `json:"code"`
	Ms      float64 `json:"ms"`
	Reply   string  `json:"reply"`
	Err     string  `json:"err"`
}

// runCall makes one query and prints its outcome; any outcome exits 0.
func runCall(args []string) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	target := fs.String("target", "", "the identity called")
	op := fs.String("op", opHello, "the op called")
	asApp := fs.Bool("as-app", false, "call as a fresh app registered on the node")
	timeout := fs.Duration("timeout", 15*time.Second, "the deadline of the call")
	if err := fs.Parse(args); err != nil {
		return err
	}
	to, err := astral.ParseIdentity(*target)
	if err != nil {
		return fmt.Errorf("-target: %w", err)
	}
	e, err := readEnv(false)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	token := e.token
	if *asApp {
		app, err := provider.Register(ctx, provider.Connect(e.endpoint, e.token), provider.Node{Endpoint: e.endpoint}, serveApps)
		if err != nil {
			return err
		}
		token = app.Token
	}
	router := apphost.NewRouter(e.endpoint, token)
	callerID, err := router.GuestIDContext(ctx)
	if err != nil {
		return fmt.Errorf("caller identity: %w", err)
	}
	caller := astrald.New(router)
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(*timeout)
	defer cancel()
	start := time.Now()
	reply, qerr := ask(cctx, caller, to, *op)
	c := newCallRecord(start, time.Now(), reply, qerr)
	out, _ := json.Marshal(callResult{Caller: callerID.String(), Target: to.String(),
		Outcome: c.Outcome, Code: c.Code, Ms: c.Ms, Reply: c.Reply, Err: c.Err})
	fmt.Println(string(out))
	return nil
}
