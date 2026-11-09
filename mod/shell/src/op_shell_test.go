package shell

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/shell"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// routedNode routes every query to router, standing in for the node's router.
type routedNode struct {
	id     *astral.Identity
	router astral.Router
}

func (n routedNode) Identity() *astral.Identity { return n.id }

func (n routedNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return n.router.RouteQuery(ctx, q, w)
}

// opProbe refuses the network origin the way node ops do (apphost.cancel,
// user.introduce) and acks every other origin.
func opProbe(ctx *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept(channel.WithOutputFormat(channel.Text))
	defer ch.Close()

	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}
	return ch.Send(&astral.Ack{})
}

// allowAuth admits every action; only Authorize is called by OpShell.
type allowAuth struct{ authmod.Module }

func (allowAuth) Authorize(*astral.Context, auth.ActionObject) bool { return true }

// probeModule returns a module that admits every caller to shell.shell and
// whose node serves opProbe as "probe".
func probeModule(t *testing.T) *Module {
	t.Helper()

	probe, err := routing.NewOp(opProbe)
	if err != nil {
		t.Fatal(err)
	}
	nodeOps := routing.NewOpRouter()
	if err := nodeOps.AddOp("probe", probe); err != nil {
		t.Fatal(err)
	}

	return &Module{
		Deps: Deps{Auth: allowAuth{}},
		node: routedNode{id: astral.GenerateIdentity(), router: nodeOps},
		log:  log.New(nil),
	}
}

// resolveDir resolves every name to id; only ResolveIdentity is called by
// OpShell.
type resolveDir struct {
	dirmod.Module
	id *astral.Identity
}

func (d resolveDir) ResolveIdentity(string) (*astral.Identity, error) { return d.id, nil }

// openShell routes a shell.shell query with origin to OpShell, sends script
// and returns everything the session wrote, and the session's prompt. A
// non-nil as opens the session with the as argument naming that identity.
func openShell(t *testing.T, origin string, as *astral.Identity, script string) (out string, prompt string) {
	t.Helper()

	mod := probeModule(t)
	shellOp, err := routing.NewOp(mod.OpShell)
	if err != nil {
		t.Fatal(err)
	}

	caller := astral.GenerateIdentity()
	guest := caller
	var args map[string]string
	if as != nil {
		mod.Deps.Dir = resolveDir{id: as}
		args = map[string]string{"as": as.String()}
		guest = as
	}
	q := astral.Launch(query.New(caller, mod.node.Identity(), "shell.shell", args))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	conn, err := query.RouteInFlight(astral.NewContext(nil), shellOp, q)
	if err != nil {
		t.Fatalf("shell.shell = %v, want accepted", err)
	}
	defer conn.Close()

	// why: the session writes its prompt before it reads, so the output is
	// drained while the script is written.
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(conn)
		done <- b
	}()
	go io.WriteString(conn, script)

	select {
	case b := <-done:
		return string(b), (&shell.Prompt{GuestID: guest, HostID: mod.node.Identity()}).Render()
	case <-time.After(2 * time.Second):
		t.Fatal("session did not end within 2s")
		return "", ""
	}
}

func TestOpShellOverALinkCarriesNetworkOriginIntoCommands(t *testing.T) {
	out, prompt := openShell(t, astral.OriginNetwork, nil, "probe\nexit\n")

	if want := prompt + "#[error_message] origin not allowed: network\nok\n" + prompt; out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestOpShellLocalRunsCommandsAsLocal(t *testing.T) {
	out, prompt := openShell(t, "", nil, "probe\nexit\n")

	if want := prompt + "#[ack] \nok\n" + prompt; out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestOpShellAsAnotherIdentityKeepsTheSessionOrigin(t *testing.T) {
	out, prompt := openShell(t, astral.OriginNetwork, astral.GenerateIdentity(), "probe\nexit\n")

	if want := prompt + "#[error_message] origin not allowed: network\nok\n" + prompt; out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}
