// Command driver runs a catalog app that serves objects.search for node1, and
// searches node1 as the User: the app's result comes back with the app as its
// source. Once the User revokes the app's searcher grant, the same search no
// longer reaches the app.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with a fresh app and word
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

const serveObjects = "mod.auth.serve_objects_action"

type node struct {
	Endpoint string `json:"endpoint"`
	HostIP   string `json:"host_ip"`
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
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_search_provider": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]bool, error) {
	facts := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1 := s.Nodes["node1"]
	if s.Facts.UserToken == "" {
		return facts, fmt.Errorf("the session holds no user_token fact")
	}
	ctx := astral.NewContext(nil)
	user := connect(n1.Endpoint, s.Facts.UserToken)

	// the catalog knows one note, under a word no other searcher answers
	word := "app-search-provider-" + astral.NewNonce().String()
	note, err := astral.ResolveObjectID(astral.NewString16("a note filed under " + word))
	if err != nil {
		return facts, err
	}

	// 1. the User registers the catalog app, asking for the searcher role alone
	roles := astral.NewBundle()
	if err := roles.Append(astral.NewString8(string(auth.RoleSearcher))); err != nil {
		return facts, err
	}
	app, err := apphostcli.New(nil, user).Register(ctx, &auth.Permit{Action: serveObjects, Constraints: roles})
	if err != nil {
		return facts, fmt.Errorf("registering the catalog app: %w", err)
	}

	// 2. the app serves objects.search and registers with node1 as a searcher
	cat := &catalog{word: word, note: note}
	stop, err := serveCatalog(ctx, n1, string(app.Token), cat)
	facts["registered_as_searcher"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("serving the catalog: %w", err)
	}
	defer stop()

	// 3. the User searches node1 for the word
	results, err := search(ctx, user, word)
	if err != nil {
		return facts, fmt.Errorf("objects.search as the User: %w", err)
	}
	facts["search_returned_the_note"] = len(results) == 1 && results[0].ObjectID.IsEqual(note)
	if !facts["search_returned_the_note"] {
		return facts, fmt.Errorf("objects.search %q answered %v; want the note %v alone", word, describe(results), note)
	}
	facts["source_is_the_app"] = results[0].SourceID.IsEqual(app.Identity)
	if !facts["source_is_the_app"] {
		return facts, fmt.Errorf("the result's source is %v; want the app %v", results[0].SourceID, app.Identity)
	}
	facts["app_was_asked_once"] = cat.asked() == 1
	if !facts["app_was_asked_once"] {
		return facts, fmt.Errorf("the app was asked %d times for %q; want once", cat.asked(), word)
	}

	// 4. without the grant, the node no longer asks the app
	// note: the node checks the grant on every fan-out and drops a searcher that
	// lost it, so this also leaves no searcher registered for later tests.
	if err := apphostcli.New(nil, user).RevokeAction(ctx, app.Identity, serveObjects); err != nil {
		return facts, fmt.Errorf("revoking the app's grant: %w", err)
	}
	results, err = search(ctx, user, word)
	if err != nil {
		return facts, fmt.Errorf("objects.search after the revoke: %w", err)
	}
	facts["revoked_app_is_not_asked"] = len(results) == 0 && cat.asked() == 1
	if !facts["revoked_app_is_not_asked"] {
		return facts, fmt.Errorf("after the revoke, objects.search answered %v and the app was asked %d times; want nothing and once",
			describe(results), cat.asked())
	}

	fmt.Fprintln(os.Stderr, "ok: the User's objects.search on node1 returned the catalog app's result, sourced to the app")
	return facts, nil
}

// catalog is the app's searcher: it answers its one word with its one note.
type catalog struct {
	word string
	note *astral.ObjectID

	count atomic.Uint64
}

func (c *catalog) SearchObject(ctx *astral.Context, q objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	if string(q.Query) == c.word {
		c.count.Add(1)
	}

	out := make(chan *objects.SearchResult)
	go func() {
		defer close(out)
		if string(q.Query) == c.word {
			_ = sig.Send(ctx, out, &objects.SearchResult{ObjectID: c.note})
		}
	}()
	return out, nil
}

func (c *catalog) asked() uint64 { return c.count.Load() }

// serveCatalog serves the app on node1 and returns once node1 has registered
// it as a searcher.
//
// note: WithObjectSearcher's lease keeper registers through the process-default
// objects client, so the app's client is also the process default here. The
// User's client stays a separate one.
func serveCatalog(ctx *astral.Context, n node, token string, cat *catalog) (func(), error) {
	router := apphost.NewRouter(n.Endpoint, token)
	apphost.SetDefaultRouter(router)
	appCli := astrald.New(router)
	astrald.SetDefault(appCli)

	sctx, cancel := ctx.WithCancel()
	registered := make(chan struct{})
	var closed atomic.Bool
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, appCli)))

	// why host_ip: the node dials the handler's registered endpoint for every
	// query, and only an address at the node's host_ip reaches the test host
	// from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(n.HostIP, "0"))
	served := make(chan error, 1)
	go func() {
		served <- apps.ServeWith(sctx, routing.NewApp(&struct{}{}), reg, at,
			apps.WithObjectSearcher(cat),
			// why last: hooks run in order, so this one runs once the searcher
			// registration before it has succeeded.
			apps.WithRegistrationHook(func(*astral.Context) error {
				if closed.CompareAndSwap(false, true) {
					close(registered)
				}
				return nil
			}),
		)
	}()

	select {
	case <-registered:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(60 * time.Second):
		cancel()
		return nil, fmt.Errorf("the app did not register within 60 s")
	}
}

// search runs objects.search for word as the given client and collects every
// result.
func search(ctx *astral.Context, cli *astrald.Client, word string) ([]*objects.SearchResult, error) {
	sctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	stream, errPtr := objectscli.New(nil, cli).Search(sctx, objects.SearchQuery{Query: astral.String16(word)})
	var results []*objects.SearchResult
	for r := range stream {
		results = append(results, r)
	}
	if errPtr != nil && *errPtr != nil {
		return results, *errPtr
	}
	return results, nil
}

func describe(results []*objects.SearchResult) []string {
	out := []string{}
	for _, r := range results {
		out = append(out, fmt.Sprintf("%v from %v", r.ObjectID, r.SourceID))
	}
	return out
}
