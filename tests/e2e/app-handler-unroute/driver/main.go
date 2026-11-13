// Command driver proves that node1 stops routing queries to an app's handler
// when the app's session ends. Each leg registers an app's handler, shows that
// a visitor's call reaches it, ends the session in one way, and shows that the
// same call now fails with route_not_found and keeps failing.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
//	driver child-app    ...        (internal) an app that serves and waits to be killed
//	driver child-binder ...        (internal) a session that binds a handler and waits to be killed
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	libapphost "github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

const (
	// removalDeadline is how long the node has to stop routing once a session ended.
	removalDeadline = 5 * time.Second
	// op is the one operation every app here serves; prefixed with the test's name.
	op = "unroute_hello"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "child-app":
		os.Exit(childApp())
	case "child-binder":
		os.Exit(childBinder())
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_handler_unroute": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(libapphost.NewRouter(ep, token)) }

// Hello is the apps' API: it answers with the label its app was built with.
type Hello struct{ label string }

// UnrouteHello answers "<label> to <caller>".
func (h *Hello) UnrouteHello(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8(h.label + " to " + q.Caller().String()))
}

// call queries the op on app as the visitor and returns the reply.
func call(ctx *astral.Context, visitor *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := visitor.WithTarget(app).QueryChannel(cctx, op, nil)
	if err != nil {
		return "", err
	}
	defer ch.Close()
	var reply *astral.String8
	if err := ch.Switch(channel.Expect(&reply), channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF()); err != nil {
		return "", err
	}
	return reply.String(), nil
}

func notFound(err error) bool { return err != nil && err.Error() == apphost.ErrCodeRouteNotFound }

// reaches reports whether a call to app is answered with the label, as the visitor.
func reaches(ctx *astral.Context, visitor *astrald.Client, visitorID, app *astral.Identity, label string) error {
	reply, err := call(ctx, visitor, app)
	if err != nil {
		return fmt.Errorf("call failed: %v", err)
	}
	if want := label + " to " + visitorID.String(); reply != want {
		return fmt.Errorf("got %q; want %q", reply, want)
	}
	return nil
}

// goneWithin polls until a call to app fails with route_not_found, within the
// deadline, and then requires the next two calls to fail the same way. Any
// other error, and any success after the first failure, is reported.
func goneWithin(ctx *astral.Context, visitor *astrald.Client, app *astral.Identity, deadline time.Duration) (time.Duration, error) {
	start := time.Now()
	for {
		_, err := call(ctx, visitor, app)
		if err != nil {
			if !notFound(err) {
				return 0, fmt.Errorf("a call after the session ended failed with %q; want %q", err, apphost.ErrCodeRouteNotFound)
			}
			break
		}
		if time.Since(start) > deadline {
			return 0, fmt.Errorf("the node still routed to the handler %v after its session ended", deadline)
		}
		time.Sleep(100 * time.Millisecond)
	}
	took := time.Since(start)
	for i := 0; i < 2; i++ {
		if _, err := call(ctx, visitor, app); !notFound(err) {
			return took, fmt.Errorf("the handler came back after it was gone: call %d returned err %v", i+2, err)
		}
	}
	return took, nil
}

type world struct {
	ctx      *astral.Context
	n1       node
	admin    *astrald.Client
	visitor  *astrald.Client
	visitID  *astral.Identity
	children []*exec.Cmd
}

// at is the address every handler of this driver binds: node1's host_ip, on a
// port the system picks.
func (w *world) at() string { return handlerAddress(w.n1.HostIP) }

// handlerAddress is a TCP address at hostIP on a port the system picks.
//
// why host_ip: the node dials a handler's registered endpoint for every query,
// and only an address at the node's host_ip reaches the test host from the
// node's machine
func handlerAddress(hostIP string) string { return "tcp:" + net.JoinHostPort(hostIP, "0") }

// newApp registers an app identity on node1 and returns it with its token.
func (w *world) newApp() (*apphost.AccessToken, error) {
	return apphostcli.New(nil, w.admin).Register(w.ctx)
}

func (w *world) reap() {
	for _, c := range w.children {
		if c.Process != nil {
			_ = c.Process.Kill()
			_ = c.Wait()
		}
	}
}

func run(path string) (facts map[string]bool, err error) {
	facts = map[string]bool{}
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
	w := &world{ctx: astral.NewContext(nil), n1: s.Nodes["node1"]}
	w.admin = connect(w.n1.Endpoint, w.n1.Token)
	defer w.reap()

	visitor, err := w.newApp()
	if err != nil {
		return facts, fmt.Errorf("registering the visitor: %w", err)
	}
	w.visitor, w.visitID = connect(w.n1.Endpoint, string(visitor.Token)), visitor.Identity

	// the bystander is an app that stays up through every leg; losing it
	// would mean the node removed more than the ended session's handlers
	bystander, err := w.newApp()
	if err != nil {
		return facts, fmt.Errorf("registering the bystander: %w", err)
	}
	stopBystander, err := serve(w.ctx, connect(w.n1.Endpoint, string(bystander.Token)), w.n1.HostIP, "bystander")
	if err != nil {
		return facts, fmt.Errorf("the bystander could not serve: %w", err)
	}
	defer stopBystander()
	stillThere := func(leg string) error {
		if err := reaches(w.ctx, w.visitor, w.visitID, bystander.Identity, "bystander"); err != nil {
			return fmt.Errorf("after %s the bystander is unreachable: %v", leg, err)
		}
		return nil
	}

	legs := []struct {
		fact string
		run  func(*world) (time.Duration, error)
	}{
		{"clean_bind_close_unroutes", (*world).cleanBindClose},
		{"clean_app_stop_unroutes", (*world).cleanAppStop},
		{"crash_app_unroutes", (*world).crashApp},
		{"crash_bind_drop_unroutes", (*world).crashBindDrop},
		{"failed_dial_removes", func(w *world) (time.Duration, error) { return 0, w.failedDial() }},
	}
	// every leg runs, so a failure names exactly the legs it breaks
	var failures []error
	bystanderOK := true
	for _, leg := range legs {
		took, err := leg.run(w)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", leg.fact, err))
		} else {
			facts[leg.fact] = true
			fmt.Fprintf(os.Stderr, "ok: %s (gone after %v)\n", leg.fact, took.Round(time.Millisecond))
		}
		if err := stillThere(leg.fact); err != nil {
			failures = append(failures, err)
			bystanderOK = false
		}
	}
	facts["bystander_unaffected"] = bystanderOK
	return facts, errors.Join(failures...)
}

// cleanBindClose: the app's bind session closes while its IPC endpoint keeps
// listening. Only the bind cleanup can unroute the handler: a dial to the
// endpoint would still succeed.
func (w *world) cleanBindClose() (time.Duration, error) {
	app, err := w.newApp()
	if err != nil {
		return 0, err
	}
	cli := apphostcli.New(nil, connect(w.n1.Endpoint, string(app.Token)))
	h, err := apps.NewHandlerOn(w.at(), astral.NewNonce())
	if err != nil {
		return 0, err
	}
	defer h.Close()
	go h.Route(w.ctx, routing.NewApp(&Hello{label: "clean-bind"}))

	bindCh, err := cli.Bind(w.ctx)
	if err != nil {
		return 0, fmt.Errorf("binding: %w", err)
	}
	defer bindCh.Close()
	if err := cli.RegisterHandler(w.ctx, h.Endpoint(), h.Token()); err != nil {
		return 0, fmt.Errorf("registering the handler: %w", err)
	}
	if err := bindCh.Send(&apphost.BindMsg{Token: h.Token()}); err != nil {
		return 0, fmt.Errorf("sending the bind token: %w", err)
	}
	if err := reaches(w.ctx, w.visitor, w.visitID, app.Identity, "clean-bind"); err != nil {
		return 0, fmt.Errorf("before the session ended: %w", err)
	}

	bindCh.Close()
	took, err := goneWithin(w.ctx, w.visitor, app.Identity, removalDeadline)
	if err != nil {
		return took, err
	}
	select {
	case <-h.Done():
		return took, errors.New("the app's endpoint closed; the leg no longer tells bind cleanup from a failed dial")
	default:
	}
	return took, nil
}

// cleanAppStop: an app served through the SDK stops cleanly (its context ends).
func (w *world) cleanAppStop() (time.Duration, error) {
	app, err := w.newApp()
	if err != nil {
		return 0, err
	}
	stop, err := serve(w.ctx, connect(w.n1.Endpoint, string(app.Token)), w.n1.HostIP, "clean-app")
	if err != nil {
		return 0, err
	}
	defer stop()
	if err := reaches(w.ctx, w.visitor, w.visitID, app.Identity, "clean-app"); err != nil {
		return 0, fmt.Errorf("before the app stopped: %w", err)
	}
	stop()
	return goneWithin(w.ctx, w.visitor, app.Identity, removalDeadline)
}

// crashApp: an app in a child process of this driver is killed with SIGKILL.
func (w *world) crashApp() (time.Duration, error) {
	app, err := w.newApp()
	if err != nil {
		return 0, err
	}
	child, err := w.spawn("child-app", []string{"NODE=" + w.n1.Endpoint, "TOKEN=" + string(app.Token), "HOST_IP=" + w.n1.HostIP})
	if err != nil {
		return 0, err
	}
	if err := reaches(w.ctx, w.visitor, w.visitID, app.Identity, "child-app"); err != nil {
		return 0, fmt.Errorf("before the app was killed: %w", err)
	}
	if err := child.Process.Kill(); err != nil {
		return 0, err
	}
	_ = child.Wait()
	return goneWithin(w.ctx, w.visitor, app.Identity, removalDeadline)
}

// crashBindDrop: a child process holds only the bind session of a handler whose
// endpoint this process keeps listening on, and is killed with SIGKILL. The
// node sees the session drop while the endpoint still answers.
func (w *world) crashBindDrop() (time.Duration, error) {
	app, err := w.newApp()
	if err != nil {
		return 0, err
	}
	h, err := apps.NewHandlerOn(w.at(), astral.NewNonce())
	if err != nil {
		return 0, err
	}
	defer h.Close()
	go h.Route(w.ctx, routing.NewApp(&Hello{label: "crash-bind"}))

	child, err := w.spawn("child-binder", []string{
		"NODE=" + w.n1.Endpoint, "TOKEN=" + string(app.Token),
		"ENDPOINT=" + h.Endpoint(), "HANDLER_TOKEN=" + h.Token().String(),
	})
	if err != nil {
		return 0, err
	}
	if err := reaches(w.ctx, w.visitor, w.visitID, app.Identity, "crash-bind"); err != nil {
		return 0, fmt.Errorf("before the session was killed: %w", err)
	}
	if err := child.Process.Kill(); err != nil {
		return 0, err
	}
	_ = child.Wait()
	took, err := goneWithin(w.ctx, w.visitor, app.Identity, removalDeadline)
	if err != nil {
		return took, err
	}
	select {
	case <-h.Done():
		return took, errors.New("the endpoint closed; the leg no longer tells bind cleanup from a failed dial")
	default:
	}
	return took, nil
}

// failedDial: a handler registered with no bind session is not removed by any
// session. Its endpoint stops answering; the failed dial removes it for good,
// so an endpoint that comes back at the same address is not routed to again.
func (w *world) failedDial() error {
	app, err := w.newApp()
	if err != nil {
		return err
	}
	cli := apphostcli.New(nil, connect(w.n1.Endpoint, string(app.Token)))
	token := astral.NewNonce()
	h, err := apps.NewHandlerOn(w.at(), token)
	if err != nil {
		return err
	}
	// the bound address, its port resolved, is where the endpoint comes back
	addr := h.Endpoint()
	defer func() { h.Close() }()
	go h.Route(w.ctx, routing.NewApp(&Hello{label: "dial-first"}))
	if err := cli.RegisterHandler(w.ctx, h.Endpoint(), token); err != nil {
		return fmt.Errorf("registering the handler: %w", err)
	}
	if err := reaches(w.ctx, w.visitor, w.visitID, app.Identity, "dial-first"); err != nil {
		return fmt.Errorf("before the endpoint closed: %w", err)
	}

	h.Close()
	if _, err := call(w.ctx, w.visitor, app.Identity); !notFound(err) {
		return fmt.Errorf("a call to a closed endpoint returned err %v; want %q", err, apphost.ErrCodeRouteNotFound)
	}

	// the same address and token answer again; a handler that was only skipped would be routed to
	h, err = apps.NewHandlerOn(addr, token)
	if err != nil {
		return fmt.Errorf("listening again on %s: %w", addr, err)
	}
	go h.Route(w.ctx, routing.NewApp(&Hello{label: "dial-second"}))
	for i := 0; i < 3; i++ {
		if reply, err := call(w.ctx, w.visitor, app.Identity); !notFound(err) {
			return fmt.Errorf("the node routed to the handler again after its dial failed: reply %q, err %v", reply, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// spawn re-executes this binary as a child with the given mode and env, and
// returns once the child printed "ready".
func (w *world) spawn(mode string, env []string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, mode)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w.children = append(w.children, cmd)

	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(out).ReadString('\n')
		if err == nil && strings.TrimSpace(line) != "ready" {
			err = fmt.Errorf("unexpected line %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			return nil, fmt.Errorf("%s did not become ready: %w", mode, err)
		}
	case <-time.After(40 * time.Second):
		return nil, fmt.Errorf("%s was not ready within 40s", mode)
	}
	return cmd, nil
}

// childApp serves the Hello API as the app NODE/TOKEN name, on a handler at
// HOST_IP, prints "ready" once the node attached its handler, and serves until
// killed.
func childApp() int {
	ctx := astral.NewContext(nil)
	if _, err := serve(ctx, connect(os.Getenv("NODE"), os.Getenv("TOKEN")), os.Getenv("HOST_IP"), "child-app"); err != nil {
		fmt.Fprintln(os.Stderr, "child-app:", err)
		return 1
	}
	fmt.Println("ready")
	select {}
}

// childBinder registers the handler at ENDPOINT/HANDLER_TOKEN, binds it to its
// own session, prints "ready", and holds the session until killed.
func childBinder() int {
	ctx := astral.NewContext(nil)
	cli := apphostcli.New(nil, connect(os.Getenv("NODE"), os.Getenv("TOKEN")))
	bindCh, err := cli.Bind(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child-binder: bind:", err)
		return 1
	}
	var tok astral.Nonce
	if err := tok.UnmarshalText([]byte(os.Getenv("HANDLER_TOKEN"))); err != nil {
		fmt.Fprintln(os.Stderr, "child-binder: token:", err)
		return 1
	}
	if err := cli.RegisterHandler(ctx, os.Getenv("ENDPOINT"), tok); err != nil {
		fmt.Fprintln(os.Stderr, "child-binder: register:", err)
		return 1
	}
	if err := bindCh.Send(&apphost.BindMsg{Token: tok}); err != nil {
		fmt.Fprintln(os.Stderr, "child-binder: bind token:", err)
		return 1
	}
	fmt.Println("ready")
	select {}
}

// serve runs the Hello API as the app cli authenticates, through its own
// registrar, on a handler at hostIP. It returns once the node has attached the
// app's handler.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, cli *astrald.Client, hostIP, label string) (func(), error) {
	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, cli)))
	attached := make(chan struct{})
	var once atomic.Bool
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		if once.CompareAndSwap(false, true) {
			close(attached)
		}
		return nil
	})
	at := apps.WithHandlerAddress(handlerAddress(hostIP))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Hello{label: label}), reg, at, onAttach) }()

	select {
	case <-attached:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(30 * time.Second):
		cancel()
		return nil, errors.New("the app's handler was not attached within 30s")
	}
}
