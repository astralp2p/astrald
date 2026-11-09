// Command driver runs a recipes app on node2 that serves objects.search and
// registers as a searcher. The User's swarm-wide search on node1 reaches the
// app once and comes back with the app's match, stamped with the app as source.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same flow with a fresh app and recipe
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// lease is the searcher registration the app asks for. It outlasts the flow
// and lapses soon after, so no later test meets the app in node2's fan-out.
const lease = astral.Duration(2 * time.Minute)

var serveObjects = &auth.Permit{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
	} `json:"facts"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := run(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_searcher_swarm": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

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

	recipes, stop, err := startRecipes(ctx, apps2, n2.Endpoint)
	if err != nil {
		return err
	}
	defer stop()
	facts["registered_as_searcher"] = true

	// the User searches the swarm from node1
	results, err := search(ctx, connect(n1.Endpoint, s.Facts.UserToken), recipes.word)
	facts["node1_search_found_the_recipe"] = err == nil && len(results) == 1 && results[0].ObjectID.IsEqual(recipes.id)
	if !facts["node1_search_found_the_recipe"] {
		return fmt.Errorf("the User's search on node1 for %q answered %v (err %v); want the recipe %v alone", recipes.word, results, err, recipes.id)
	}
	facts["result_names_the_app_as_source"] = results[0].SourceID.IsEqual(recipes.app)
	if !facts["result_names_the_app_as_source"] {
		return fmt.Errorf("the match came from source %v; want the recipes app %v", results[0].SourceID, recipes.app)
	}

	// the app was asked once, with the User's words, by its own node
	asked := recipes.searches()
	facts["app_asked_once"] = len(asked) == 1 && asked[0].query == recipes.word && asked[0].caller.IsEqual(node2)
	if !facts["app_asked_once"] {
		return fmt.Errorf("the recipes app was asked %v; want %q once, by node2 %v", asked, recipes.word, node2)
	}
	fmt.Fprintln(os.Stderr, "ok: a swarm search on node1 was answered once by the recipes app on node2")
	return nil
}

// refuseWithoutGrant checks that an app without serve_objects cannot become a searcher.
func refuseWithoutGrant(ctx *astral.Context, apps2 *apphostcli.Client, endpoint string, facts map[string]bool) error {
	plain, err := apps2.Register(ctx)
	if err != nil {
		return fmt.Errorf("register the plain app: %w", err)
	}
	_, err = objectscli.New(nil, connect(endpoint, string(plain.Token))).RegisterSearcher(ctx, lease)
	facts["searcher_refused_without_grant"] = err != nil && err.Error() == auth.ErrNotPermitted.Error()
	if !facts["searcher_refused_without_grant"] {
		return fmt.Errorf("objects.register_searcher for an app without serve_objects answered %v; want %v", err, auth.ErrNotPermitted)
	}
	return nil
}

// startRecipes registers the recipes app with serve_objects, gives it one
// recipe, and serves it until the returned stop runs.
func startRecipes(ctx *astral.Context, apps2 *apphostcli.Client, endpoint string) (*recipes, func(), error) {
	token, err := apps2.Register(ctx, serveObjects)
	if err != nil {
		return nil, nil, fmt.Errorf("register the recipes app: %w", err)
	}
	// why a fresh word per run: a match from anything but this app's recipe cannot share it.
	word := "pancakes" + astral.NewNonce().String()
	r := &recipes{app: token.Identity, word: word}
	if r.id, err = astral.Resolve(bytes.NewReader([]byte("a recipe for " + word))); err != nil {
		return nil, nil, err
	}
	stop, err := r.serve(ctx, endpoint, string(token.Token))
	if err != nil {
		return nil, nil, err
	}
	return r, stop, nil
}

// recipes is an app that knows one recipe and finds it by one word.
type recipes struct {
	app  *astral.Identity
	id   *astral.ObjectID
	word string

	mu    sync.Mutex
	asked []asked
}

type asked struct {
	query  string
	caller *astral.Identity
}

func (a asked) String() string { return fmt.Sprintf("%q by %v", a.query, a.caller) }

type searchArgs struct {
	Query string `query:"key:q"`
	Out   string
}

// Search matches the recipe when the query's words are the app's word.
func (r *recipes) Search(_ *astral.Context, q *routing.IncomingQuery, args searchArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var sq objects.SearchQuery
	_ = sq.UnmarshalText([]byte(args.Query))
	r.mu.Lock()
	r.asked = append(r.asked, asked{query: string(sq.Query), caller: q.Caller()})
	r.mu.Unlock()

	if string(sq.Query) == r.word {
		if err := ch.Send(&objects.SearchResult{ObjectID: r.id}); err != nil {
			return err
		}
	}
	return ch.Send(&astral.EOS{})
}

func (r *recipes) searches() []asked {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]asked(nil), r.asked...)
}

// serve runs the app on node2 under its own client and returns once node2 has
// granted it a searcher lease.
func (r *recipes) serve(ctx *astral.Context, endpoint, token string) (func(), error) {
	// note: the handler's listener takes its protocol from the default router.
	apphost.SetDefaultRouter(apphost.NewRouter(endpoint, token))
	client := connect(endpoint, token)

	app := routing.NewApp(&struct{}{})
	app.Add("objects", r)

	registered := make(chan error, 1)
	hook := func(ctx *astral.Context) error {
		_, err := objectscli.New(nil, client).RegisterSearcher(ctx, lease)
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
			return nil, fmt.Errorf("objects.register_searcher: %w", err)
		}
	case err := <-served:
		cancel()
		return nil, fmt.Errorf("serve the recipes app: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		return nil, fmt.Errorf("the recipes app did not register as a searcher within 30s")
	}
	return cancel, nil
}

// search runs objects.search for word across the swarm (zone=dvn) and returns
// every result in the stream.
func search(ctx *astral.Context, c *astrald.Client, word string) ([]*objects.SearchResult, error) {
	sctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(60 * time.Second)
	defer cancel()
	ch, err := c.QueryChannel(sctx, objects.MethodSearch, query.Args{"q": word, "zone": "dvn"})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	var results []*objects.SearchResult
	err = ch.Switch(
		func(res *objects.SearchResult) error {
			results = append(results, res)
			return nil
		},
		channel.BreakOnEOS,
		channel.PassErrors,
		channel.WithContext(sctx),
	)
	return results, err
}
