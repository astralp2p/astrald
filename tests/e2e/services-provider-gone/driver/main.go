// Command driver tells one story across node1 and node2: a consumer app on
// node1 follows the spg-echo service with reach=swarm while a provider app on
// node2 stops cleanly, restarts, crashes and restarts again. Each loss is a
// removal on the follow, each restart shows the provider again.
//
// The provider runs as a child process of the driver (this same binary, started
// through os.Executable) so that it can be killed.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
//	driver provider                the child: serves the app named by SPG_* variables
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

const service = "spg-echo"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "provider" {
		os.Exit(runProvider())
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"services_provider_gone": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// runProvider is the child: it serves the app whose endpoint, token and
// identity SPG_ENDPOINT, SPG_TOKEN and SPG_IDENTITY name, on a handler bound at
// SPG_HOST_IP, prints "ready" once the node holds its advertisement, and on
// SIGTERM stops cleanly.
func runProvider() int {
	id, err := astral.ParseIdentity(os.Getenv("SPG_IDENTITY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider:", err)
		return 1
	}
	token := os.Getenv("SPG_TOKEN")
	ep := os.Getenv("SPG_ENDPOINT")
	n := node{Endpoint: ep, Token: token, HostIP: os.Getenv("SPG_HOST_IP")}
	a := &app{id: id, client: connect(ep, token), n: n, token: token}
	e, err := startEcho(astral.NewContext(nil), a, service)
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider:", err)
		return 1
	}
	fmt.Println("ready")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	<-sig
	e.stop()
	return 0
}

// child is a running provider process.
type child struct {
	cmd  *exec.Cmd
	done chan error
}

// startChild starts the provider process for a on node n and waits until it
// reports ready.
func startChild(a *app) (*child, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "provider")
	cmd.Env = append(os.Environ(), "SPG_ENDPOINT="+a.n.Endpoint, "SPG_HOST_IP="+a.n.HostIP, "SPG_TOKEN="+a.token, "SPG_IDENTITY="+a.id.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &child{cmd: cmd, done: make(chan error, 1)}
	ready := make(chan bool, 1)
	go func() {
		line, _ := bufio.NewReader(out).ReadString('\n')
		ready <- strings.TrimSpace(line) == "ready"
	}()
	go func() { c.done <- cmd.Wait() }()
	select {
	case ok := <-ready:
		if !ok {
			c.kill()
			return nil, fmt.Errorf("provider process for %v did not report ready", a.id)
		}
		return c, nil
	case <-time.After(90 * time.Second):
		c.kill()
		return nil, fmt.Errorf("provider process for %v not ready within 90s", a.id)
	}
}

// stop asks the process to stop cleanly and waits for it to exit 0.
func (c *child) stop() error {
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	return c.wait(20 * time.Second)
}

// kill ends the process with SIGKILL: no cleanup runs.
func (c *child) kill() {
	_ = c.cmd.Process.Kill()
	_ = c.wait(20 * time.Second)
}

func (c *child) wait(d time.Duration) error {
	select {
	case err := <-c.done:
		c.done <- err
		return err
	case <-time.After(d):
		return fmt.Errorf("provider process did not exit within %v", d)
	}
}

func story(path string, facts map[string]bool) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	n1, n2 := nodes["node1"], nodes["node2"]
	ctx := astral.NewContext(nil)

	// the consumer: an app on node1 that node1's admin grants discovery of spg-echo
	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}
	f, err := follow(ctx, consumer, services.ReachSwarm, service)
	if err != nil {
		return err
	}
	defer f.stop()

	ev, err := f.next()
	empty := err == nil && ev.Initial != nil && ev.Initial.Complete
	if err := step(facts, "follow_starts_empty", empty, "first follow event %+v, err %v; want a complete initial outcome with no offering", ev, err); err != nil {
		return err
	}

	// the provider: one app identity on node2, served by a process of its own
	prov, err := register(ctx, n2, serveApps)
	if err != nil {
		return err
	}

	c, err := startChild(prov)
	if err != nil {
		return err
	}
	defer c.kill()
	ev, err = f.next()
	if err := step(facts, "follow_shows_the_provider", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v", ev, err, prov.id, service); err != nil {
		return err
	}

	// a clean stop: the process closes its advertisement and its session
	if err := c.stop(); err != nil {
		return fmt.Errorf("stopping the provider cleanly: %v", err)
	}
	ev, err = f.next()
	if err := step(facts, "clean_stop_removes_the_provider", removes(ev, err, prov.id), "follow event %+v, err %v; want the removal of %v offering %v", ev, err, prov.id, service); err != nil {
		return err
	}
	if err := goneFromSwarm(ctx, facts, "clean_stop_leaves_no_provider", consumer, prov.id); err != nil {
		return err
	}

	// a restart under the same identity: the follow shows the provider again
	c, err = startChild(prov)
	if err != nil {
		return err
	}
	defer c.kill()
	ev, err = f.next()
	if err := step(facts, "restart_shows_the_provider_again", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v again", ev, err, prov.id, service); err != nil {
		return err
	}

	// a crash: SIGKILL, no cleanup; the node sees the session drop
	c.kill()
	ev, err = f.next()
	if err := step(facts, "crash_removes_the_provider", removes(ev, err, prov.id), "follow event %+v, err %v; want the removal of %v offering %v", ev, err, prov.id, service); err != nil {
		return err
	}
	if err := goneFromSwarm(ctx, facts, "crash_leaves_no_provider", consumer, prov.id); err != nil {
		return err
	}

	// a restart after the crash: shown again
	c, err = startChild(prov)
	if err != nil {
		return err
	}
	defer c.kill()
	ev, err = f.next()
	if err := step(facts, "restart_after_crash_shows_the_provider", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v again", ev, err, prov.id, service); err != nil {
		return err
	}

	// leave nothing behind: stop the provider; the follow ends the story
	if err := c.stop(); err != nil {
		return fmt.Errorf("stopping the provider cleanly: %v", err)
	}
	ev, err = f.next()
	return step(facts, "final_stop_removes_the_provider", removes(ev, err, prov.id), "follow event %+v, err %v; want the removal of %v offering %v", ev, err, prov.id, service)
}

// goneFromSwarm records, under name, that a fresh one-shot swarm discovery of
// the service from consumer completes and names no provider.
func goneFromSwarm(ctx *astral.Context, facts map[string]bool, name string, consumer *app, gone *astral.Identity) error {
	d := discoverOnce(ctx, consumer, services.ReachSwarm, service)
	return step(facts, name, d.complete() && len(d.providers) == 0, "one-shot swarm discovery of %v after %v went: providers %v, outcome %+v, err %v; want a complete outcome with none", service, gone, d.providers, d.outcome, d.err)
}

// shows reports whether ev is the available offering of service by provider.
func shows(ev servicescli.Event, err error, provider *astral.Identity) bool {
	return err == nil && ev.Update != nil && bool(ev.Update.Available) &&
		ev.Update.ProviderID.IsEqual(provider) && string(ev.Update.Name) == service
}

// removes reports whether ev removes exactly the provider's offering of service.
func removes(ev servicescli.Event, err error, provider *astral.Identity) bool {
	if err != nil || len(ev.Removed) != 1 {
		return false
	}
	k := ev.Removed[0]
	return k.ProviderID.IsEqual(provider) && string(k.Name) == service
}
