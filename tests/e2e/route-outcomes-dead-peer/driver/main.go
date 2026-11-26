// Command driver waits until node1's traffic to node2 has settled, freezes
// node2's astrald while an app on node1 queries it, then kills it while
// another query waits on the link, and records how each query ended and when.
//
//	driver <session.json>   prints facts as JSON
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// settleQuiet is how long node1's log stays unchanged before the driver
// treats the traffic to node2 as settled. It outlasts the 5 s wait that
// precedes a node's endpoint announcement (astral-docs protocols/nodes,
// Announcing a change) and the push that follows it.
const settleQuiet = 7 * time.Second

// settleLimit bounds the wait for node1's log to go quiet.
const settleLimit = 60 * time.Second

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	Root     string `json:"root"`
}

// outcome is how one query ended.
type outcome struct {
	Kind      string  `json:"kind"` // accepted, rejected, or the apphost error code
	Code      int     `json:"code"` // the reject code when Kind is rejected
	Retryable bool    `json:"retryable"`
	Millis    float64 `json:"millis"`
	Error     string  `json:"error"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[1])
	out, _ := json.Marshal(map[string]any{"dead_peer": facts})
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
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return facts, err
	}
	pid, err := findPid(n2.Root)
	if err != nil {
		return facts, err
	}
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	app, err := contracts.Register(ctx, admin1, n1.Endpoint)
	if err != nil {
		return facts, fmt.Errorf("registering the caller on node1: %w", err)
	}
	// why: node2 admits a relay for the app only once it holds the app's
	// relay contract, which apphost.register pushes in the background.
	facts["node2_knows_the_caller"] = waitHost(ctx, admin2, app.Identity, n1.Identity)
	caller := connect(n1.Endpoint, string(app.Token))
	facts["live"] = ask(ctx, caller, node2, "no.such.op")

	// why: a push that node1 sends to a frozen node2 times out after 15 s, and
	// the timeout wakes the link's ping, which closes the link 10 s later
	// (astral-docs topics/link-mux.md, early probes) and ends the frozen query
	// as unavailable. A fresh world still holds the pushes that follow
	// node2's adoption and first link, so node2 freezes only after they end.
	settled, err := waitLogQuiet(filepath.Join(n1.Root, "astrald.log"), settleQuiet, settleLimit)
	if err != nil {
		return facts, fmt.Errorf("node1's traffic to node2 never settled: %w", err)
	}
	facts["settled_millis"] = float64(settled.Microseconds()) / 1000

	// a frozen node keeps its link open and never answers
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return facts, fmt.Errorf("freezing node2 (pid %v): %w", pid, err)
	}
	facts["frozen"] = ask(ctx, caller, node2, "no.such.op")

	// a query waits on the frozen link while node2 dies
	waiting := make(chan outcome, 1)
	go func() { waiting <- ask(ctx, caller, node2, "no.such.op") }()
	time.Sleep(3 * time.Second)
	killedAt := time.Now()
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return facts, fmt.Errorf("killing node2 (pid %v): %w", pid, err)
	}
	facts["killed"] = <-waiting
	facts["answered_after_kill_millis"] = float64(time.Since(killedAt).Microseconds()) / 1000
	return facts, nil
}

// waitLogQuiet waits until the log file at path has not changed for quiet,
// within limit, and returns how long it waited.
// note: node1 logs each push it routes to node2 and each link it adds, so a
// quiet log means no post-link push is pending or in flight.
func waitLogQuiet(path string, quiet, limit time.Duration) (time.Duration, error) {
	start := time.Now()
	for time.Since(start) < limit {
		info, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		if time.Since(info.ModTime()) >= quiet {
			return time.Since(start), nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0, fmt.Errorf("%v kept changing for %v", path, limit)
}

// findPid finds the astrald process that runs with root.
func findPid(root string) (int, error) {
	want := []byte("-root\x00" + root + "\x00")
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		cmd, err := os.ReadFile(e)
		if err != nil || !bytes.Contains(cmd, want) {
			continue
		}
		return strconv.Atoi(filepath.Base(filepath.Dir(e)))
	}
	return 0, fmt.Errorf("no astrald process runs with root %v", root)
}

// ask queries target:op as the caller and records how the query ended.
func ask(ctx *astral.Context, caller *astrald.Client, target *astral.Identity, op string) outcome {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(90 * time.Second)
	defer cancel()
	start := time.Now()
	conn, err := caller.WithTarget(target).Query(cctx, op, nil)
	o := outcome{Millis: float64(time.Since(start).Microseconds()) / 1000, Retryable: apphost.IsRetryable(err)}
	if err == nil {
		conn.Close()
		o.Kind = "accepted"
		return o
	}
	o.Error = err.Error()
	var rejected *astral.ErrRejected
	var msg *apphostapi.ErrorMsg
	switch {
	case errors.As(err, &rejected):
		o.Kind, o.Code = "rejected", int(rejected.Code)
	case errors.As(err, &msg):
		o.Kind = string(msg.Code)
	default:
		o.Kind = "other"
	}
	return o
}

// waitHost polls apphost.hosts on the admin's node until it names host for
// app, within 30 seconds.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		found := false
		for h := range hosts {
			found = found || h.String() == host
		}
		if *errp == nil && found {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
