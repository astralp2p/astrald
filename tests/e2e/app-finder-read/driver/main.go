// Command driver runs a notes app on node2 that holds a note no repository
// holds, serves objects.find and objects.read for it, and registers as a finder.
// A read on node2, and a read the User makes on node1, both come back with the
// note's bytes, served by the app.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same flow with a fresh app and note
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// lease is the finder registration the app asks for. It outlasts the flow and
// lapses soon after, so no later test meets the app in node2's fan-out.
const lease = astral.Duration(2 * time.Minute)

var serveObjects = &auth.Permit{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}

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
	facts := map[string]bool{}
	err := run(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_finder_read": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
	} `json:"facts"`
}

func run(path string, facts map[string]bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	apps2 := apphostcli.New(nil, connect(n2.Endpoint, n2.Token))

	if err := refuseWithoutGrant(ctx, apps2, n2.Endpoint, facts); err != nil {
		return err
	}

	notes, stop, err := startNotes(ctx, apps2, n2.Endpoint)
	if err != nil {
		return err
	}
	defer stop()
	facts["registered_as_finder"] = true

	// node2's find names the app
	found, err := findOn(ctx, connect(n2.Endpoint, n2.Token), notes.id)
	facts["find_names_the_app"] = err == nil && len(found) == 1 && found[0].IsEqual(notes.app)
	if !facts["find_names_the_app"] {
		return fmt.Errorf("objects.find on node2 named %v (err %v); want the notes app %v alone", found, err, notes.app)
	}

	// a read on node2 comes back with the app's bytes
	got, err := read(ctx, objectscli.New(nil, connect(n2.Endpoint, n2.Token)), notes.id)
	facts["node2_read_the_note"] = err == nil && bytes.Equal(got, notes.text)
	if !facts["node2_read_the_note"] {
		return fmt.Errorf("objects.read on node2 answered %q (err %v); want %q", got, err, notes.text)
	}

	// the User's read on node1, for node2, comes back with the app's bytes
	got, err = read(ctx, objectscli.New(node2, connect(n1.Endpoint, s.Facts.UserToken)), notes.id)
	facts["node1_read_the_note"] = err == nil && bytes.Equal(got, notes.text)
	if !facts["node1_read_the_note"] {
		return fmt.Errorf("the User's objects.read on node1 for node2 answered %q (err %v); want %q", got, err, notes.text)
	}

	// both reads reached the app, each from node2
	callers := notes.readers()
	facts["app_served_both_reads"] = len(callers) == 2 && callers[0].IsEqual(node2) && callers[1].IsEqual(node2)
	if !facts["app_served_both_reads"] {
		return fmt.Errorf("the notes app served reads to %v; want node2 %v twice", callers, node2)
	}
	fmt.Fprintln(os.Stderr, "ok: reads on node2 and node1 were answered from the notes app")
	return nil
}

// refuseWithoutGrant checks that an app without serve_objects cannot become a finder.
func refuseWithoutGrant(ctx *astral.Context, apps2 *apphostcli.Client, endpoint string, facts map[string]bool) error {
	plain, err := apps2.Register(ctx)
	if err != nil {
		return fmt.Errorf("register the plain app: %w", err)
	}
	_, err = objectscli.New(nil, connect(endpoint, string(plain.Token))).RegisterFinder(ctx, lease)
	facts["finder_refused_without_grant"] = err != nil && err.Error() == auth.ErrNotPermitted.Error()
	if !facts["finder_refused_without_grant"] {
		return fmt.Errorf("objects.register_finder for an app without serve_objects answered %v; want %v", err, auth.ErrNotPermitted)
	}
	return nil
}

// startNotes registers the notes app with serve_objects, gives it one note,
// and serves it until the returned stop runs.
func startNotes(ctx *astral.Context, apps2 *apphostcli.Client, endpoint string) (*notes, func(), error) {
	token, err := apps2.Register(ctx, serveObjects)
	if err != nil {
		return nil, nil, fmt.Errorf("register the notes app: %w", err)
	}
	n := &notes{app: token.Identity, text: []byte("a note the notes app alone holds, " + astral.NewNonce().String())}
	if n.id, err = astral.Resolve(bytes.NewReader(n.text)); err != nil {
		return nil, nil, err
	}
	stop, err := n.serve(ctx, endpoint, string(token.Token))
	if err != nil {
		return nil, nil, err
	}
	return n, stop, nil
}

// notes is an app that holds one note in memory and serves it as an object.
type notes struct {
	app  *astral.Identity
	id   *astral.ObjectID
	text []byte

	mu      sync.Mutex
	callers []*astral.Identity
}

type findArgs struct {
	ID  *astral.ObjectID `query:"required"`
	Out string
}

// Find names the app itself for its note and nothing for any other object.
func (n *notes) Find(_ *astral.Context, q *routing.IncomingQuery, args findArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	if args.ID.IsEqual(n.id) {
		if err := ch.Send(n.app); err != nil {
			return err
		}
	}
	return ch.Send(&astral.EOS{})
}

type readArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Offset astral.Uint64
	Limit  astral.Uint64
}

// Read streams the note's raw bytes and rejects any other object.
func (n *notes) Read(_ *astral.Context, q *routing.IncomingQuery, args readArgs) error {
	if !args.ID.IsEqual(n.id) {
		return q.Reject()
	}
	n.mu.Lock()
	n.callers = append(n.callers, q.Caller())
	n.mu.Unlock()

	end := uint64(len(n.text))
	if args.Limit > 0 && uint64(args.Offset)+uint64(args.Limit) < end {
		end = uint64(args.Offset) + uint64(args.Limit)
	}
	if uint64(args.Offset) > end {
		return q.Reject()
	}
	conn := q.AcceptRaw()
	defer conn.Close()
	_, err := conn.Write(n.text[args.Offset:end])
	return err
}

func (n *notes) readers() []*astral.Identity {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*astral.Identity(nil), n.callers...)
}

// serve runs the app on node2 under its own client and returns once node2 has
// granted it a finder lease.
func (n *notes) serve(ctx *astral.Context, endpoint, token string) (func(), error) {
	// note: the handler's listener takes its protocol from the default router.
	apphost.SetDefaultRouter(apphost.NewRouter(endpoint, token))
	client := connect(endpoint, token)

	app := routing.NewApp(&struct{}{})
	app.Add("objects", n)

	registered := make(chan error, 1)
	hook := func(ctx *astral.Context) error {
		_, err := objectscli.New(nil, client).RegisterFinder(ctx, lease)
		select {
		case registered <- err:
		default:
		}
		return err
	}

	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, client)))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, app, reg, apps.WithRegistrationHook(hook)) }()

	select {
	case err := <-registered:
		if err != nil {
			cancel()
			return nil, fmt.Errorf("objects.register_finder: %w", err)
		}
	case err := <-served:
		cancel()
		return nil, fmt.Errorf("serve the notes app: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		return nil, fmt.Errorf("the notes app did not register as a finder within 30s")
	}
	return cancel, nil
}

// findOn runs objects.find on a node and returns the identities it named.
func findOn(ctx *astral.Context, c *astrald.Client, id *astral.ObjectID) ([]*astral.Identity, error) {
	fctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	providers, errPtr := objectscli.New(nil, c).Find(fctx, id)
	var found []*astral.Identity
	for p := range providers {
		found = append(found, p)
	}
	return found, *errPtr
}

// read runs objects.read with no repository, so the node reads from what its
// finders name, and returns the bytes.
func read(ctx *astral.Context, c *objectscli.Client, id *astral.ObjectID) ([]byte, error) {
	rctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(60 * time.Second)
	defer cancel()
	r, err := c.Read(rctx, id, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
