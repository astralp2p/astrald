// Command driver appoints a manager on an unclaimed node that answers the
// swarm invite policy, and has the swarm's User node invite the node.
//
//	driver drive <session.json>   prints facts as JSON
//
// The User's token on the inviter arrives in MAI_USER_TOKEN, so it never
// shows on a command line.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/sys"
	syscli "github.com/astralp2p/astral-go/api/sys/client"
	"github.com/astralp2p/astral-go/api/user"
	usercli "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

const (
	// opDecide is the operation the node asks about an invitation; the
	// manager's appointment binds it to the swarm_invite policy.
	opDecide = "mai.decide"
	opPing   = "mai.ping"
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

// asked is what the manager's handler saw of one invitation.
type asked struct {
	Inviter string `json:"inviter"`
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// outcome is what one case records.
type outcome struct {
	Manager        string  `json:"manager"`
	StateBefore    string  `json:"state_before"`
	ManagerBefore  string  `json:"manager_before"`
	Asked          []asked `json:"asked"`
	ServeError     string  `json:"serve_error"`
	AdoptError     string  `json:"adopt_error"`
	AdoptedSubject string  `json:"adopted_subject"`
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "drive" {
		fmt.Fprintln(os.Stderr, "usage: driver drive <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	out, _ := json.Marshal(map[string]any{"cases": facts})
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]*outcome, error) {
	facts := map[string]*outcome{}
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
	inviter := s.Nodes["mai-inviter"]
	userToken := os.Getenv("MAI_USER_TOKEN")
	if userToken == "" {
		return facts, errors.New("MAI_USER_TOKEN is empty")
	}
	ctx := astral.NewContext(nil)
	userCli := connect(inviter.Endpoint, userToken)

	for _, c := range []struct {
		name  string
		node  string
		allow bool
	}{{"approve", "mai-approve", true}, {"decline", "mai-decline", false}} {
		o := &outcome{}
		facts[c.name] = o
		if err := runCase(ctx, s.Nodes[c.node], inviter, userCli, c.allow, o); err != nil {
			return facts, fmt.Errorf("%s: %w", c.name, err)
		}
		fmt.Fprintf(os.Stderr, "ok: %s: the manager was asked %d time(s); adopt: %q\n", c.name, len(o.Asked), o.AdoptError)
	}
	return facts, nil
}

// runCase appoints a manager on n that decides every invitation as allow,
// has the inviter's User adopt n, and records what the manager saw and how
// the adoption ended.
func runCase(ctx *astral.Context, n, inviter node, userCli *astrald.Client, allow bool, o *outcome) error {
	admin := connect(n.Endpoint, n.Token)

	before, err := usercli.New(nil, admin).Info(ctx)
	if err != nil {
		return fmt.Errorf("user.info: %w", err)
	}
	if before.State != user.StateFresh {
		return fmt.Errorf("the node is %q; want fresh", before.State)
	}

	tok, err := syscli.New(nil, admin).RegisterManager(ctx, &sys.RegisterManagerRequest{
		Name:     "mai-manager",
		Policies: []*sys.PolicyBinding{{Policy: sys.PolicySwarmInvite, Operation: opDecide}},
	})
	if err != nil {
		return fmt.Errorf("sys.register_manager: %w", err)
	}
	o.Manager = tok.Identity.String()

	mgr := connect(n.Endpoint, string(tok.Token))
	app := &manager{allow: allow}
	stop, err := serve(ctx, mgr, n.HostIP, app)
	if err != nil {
		// why a fact: whether the manager can bind and attach a handler on the
		// unclaimed node is what the oracle judges, and the invitation is
		// pointless without it.
		o.ServeError = err.Error()
		return nil
	}
	defer stop()

	// why the node's token: a query the node sends the manager passes the
	// gate, and this one shows the handler routable before the invitation.
	pctx, cancel := ctx.WithTimeout(15 * time.Second)
	defer cancel()
	if _, err := admin.WithTarget(tok.Identity).QueryChannel(pctx, opPing, nil); err != nil {
		o.ServeError = "not routable: " + err.Error()
		return nil
	}

	mid, err := usercli.New(nil, admin).Info(ctx)
	if err != nil {
		return fmt.Errorf("user.info: %w", err)
	}
	o.StateBefore = string(mid.State)
	if mid.Manager != nil {
		o.ManagerBefore = mid.Manager.String()
	}

	actx, acancel := ctx.WithTimeout(60 * time.Second)
	defer acancel()
	signed, err := usercli.New(nil, userCli).Adopt(actx, n.Identity)
	if err != nil {
		o.AdoptError = err.Error()
	} else {
		o.AdoptedSubject = signed.Subject.String()
	}
	o.Asked = app.seen()
	return nil
}

// manager is the app the node delegates swarm_invite to.
type manager struct {
	allow bool
	log   atomic.Pointer[[]asked]
}

func (m *manager) seen() []asked {
	if p := m.log.Load(); p != nil {
		return append([]asked{}, *p...)
	}
	return []asked{}
}

// Decide answers one invitation as the manager is told to.
func (m *manager) Decide(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	obj, err := ch.Receive()
	if err != nil {
		return err
	}
	req, ok := obj.(*user.SwarmInviteRequest)
	if !ok {
		return fmt.Errorf("asked %T; want a swarm invite request", obj)
	}
	log := m.seen()
	log = append(log, asked{
		Inviter: req.Inviter.String(),
		Issuer:  req.Contract.Issuer.String(),
		Subject: req.Contract.Subject.String(),
	})
	m.log.Store(&log)
	return ch.Send(&user.SwarmInviteDecision{Allow: astral.Bool(m.allow)})
}

// Ping answers Ack.
func (m *manager) Ping(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(&astral.Ack{})
}

// serve runs the manager's ops as the app cli authenticates, through its own
// registrar, on a handler at hostIP. It returns once the node has attached the
// app's handler.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, cli *astrald.Client, hostIP string, m *manager) (func(), error) {
	ops := routing.NewOpRouter()
	for name, fn := range map[string]any{opDecide: m.Decide, opPing: m.Ping} {
		op, err := routing.NewOp(fn)
		if err != nil {
			return nil, err
		}
		if err := ops.AddOp(name, op); err != nil {
			return nil, err
		}
	}
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
	// why host_ip: the node dials the handler's registered endpoint for every
	// query, and only an address at the node's host_ip reaches the test host
	// from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(hostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, ops, reg, at, onAttach) }()

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
