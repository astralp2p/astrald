// Command driver runs an indexer app for node1: the User stores a note in a
// repository with indexing enabled, and the app is told about it. Once the User
// revokes the app's indexer grant, the app can no longer subscribe.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with a fresh app and repository
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/indexing"
	indexingcli "github.com/astralp2p/astral-go/api/indexing/client"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

const serveObjects = "mod.auth.serve_objects_action"

// memSize is the capacity of the test's memory repository, 1 MiB.
const memSize = 1 << 20

type node struct {
	Endpoint string `json:"endpoint"`
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
		out, _ := json.Marshal(map[string]any{"app_index_provider": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (facts map[string]bool, err error) {
	facts = map[string]bool{}
	// why: state left behind changes what later tests in the shared world
	// observe, so a cleanup step that fails fails the driver.
	var cleanup []error
	defer func() { err = errors.Join(append([]error{err}, cleanup...)...) }()
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
	name := "app-index-provider-" + astral.NewNonce().String()

	// 1. the User creates a memory repository and turns indexing on for it
	if err := objectscli.New(nil, user).NewMem(ctx, name, memSize); err != nil {
		return facts, fmt.Errorf("creating repository %v: %w", name, err)
	}
	defer func() { cleanup = append(cleanup, removeRepo(ctx, user, name)) }()
	if err := indexingcli.New(nil, user).EnableRepo(ctx, name); err != nil {
		return facts, fmt.Errorf("enabling indexing on %v: %w", name, err)
	}
	defer func() { cleanup = append(cleanup, disableRepo(ctx, user, name)) }()

	// 2. the User registers the indexer app, asking for the indexer role alone
	roles := astral.NewBundle()
	if err := roles.Append(astral.NewString8(string(auth.RoleIndexer))); err != nil {
		return facts, err
	}
	app, err := apphostcli.New(nil, user).Register(ctx, &auth.Permit{Action: serveObjects, Constraints: roles})
	if err != nil {
		return facts, fmt.Errorf("registering the indexer app: %w", err)
	}
	idx := indexingcli.New(nil, connect(n1.Endpoint, string(app.Token)))

	// 3. the app registers an indexer and subscribes to its changes
	nonce, err := idx.RegisterIndexer(ctx, name)
	facts["registered_indexer"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("indexing.register_indexer as the app: %w", err)
	}
	defer func() { cleanup = append(cleanup, unregister(ctx, idx, nonce)) }()
	sctx, closeSub := ctx.WithCancel()
	defer closeSub()
	sub, err := idx.Subscribe(sctx, nonce)
	if err != nil {
		return facts, fmt.Errorf("indexing.subscribe as the app: %w", err)
	}

	// 4. the User stores a note, and the app is told about it
	note, err := objectscli.New(nil, user).Store(ctx, name, astral.NewString16("a note for "+name))
	if err != nil {
		return facts, fmt.Errorf("storing the note in %v: %w", name, err)
	}
	msg, err := nextChange(sub, 30*time.Second)
	if err != nil {
		return facts, fmt.Errorf("waiting for the note's change: %w", err)
	}
	told, ok := msg.(*indexing.IndexMsg)
	facts["told_about_the_note"] = ok && string(told.Repo) == name && told.ObjectID.IsEqual(note)
	if !facts["told_about_the_note"] {
		return facts, fmt.Errorf("the app was sent %#v; want indexing.index of %v in %v", msg, note, name)
	}
	// note: the ack commits the app's cursor past the note; the cursor itself is
	// indexer-cursor's subject.
	if err := sub.Ack(); err != nil {
		return facts, fmt.Errorf("acking the note's change: %w", err)
	}
	closeSub()

	// 5. without the grant, the app can no longer subscribe
	if err := apphostcli.New(nil, user).RevokeAction(ctx, app.Identity, serveObjects); err != nil {
		return facts, fmt.Errorf("revoking the app's grant: %w", err)
	}
	refused, err := subscribeRefusal(ctx, idx, nonce)
	facts["revoked_app_cannot_subscribe"] = errors.Is(refused, auth.ErrNotPermitted)
	if !facts["revoked_app_cannot_subscribe"] {
		return facts, fmt.Errorf("after the revoke, indexing.subscribe answered %v (err %v); want %v", refused, err, auth.ErrNotPermitted)
	}

	fmt.Fprintln(os.Stderr, "ok: the indexer app was told about the note the User stored on node1")
	return facts, nil
}

// nextChange waits up to timeout for the subscription's next change.
func nextChange(sub *indexingcli.Subscription, timeout time.Duration) (astral.Object, error) {
	type answer struct {
		obj astral.Object
		err error
	}
	got := make(chan answer, 1)
	go func() {
		obj, err := sub.Next()
		got <- answer{obj, err}
	}()
	select {
	case a := <-got:
		return a.obj, a.err
	case <-time.After(timeout):
		_ = sub.Close()
		return nil, fmt.Errorf("no change within %v", timeout)
	}
}

// subscribeRefusal subscribes once more and returns the error node1 answers
// with first.
func subscribeRefusal(ctx *astral.Context, idx *indexingcli.Client, nonce astral.Nonce) (error, error) {
	sctx, cancel := ctx.WithCancel()
	defer cancel()
	sub, err := idx.Subscribe(sctx, nonce)
	if err != nil {
		return nil, err
	}
	msg, err := nextChange(sub, 30*time.Second)
	if msg != nil {
		return nil, fmt.Errorf("node1 sent %#v", msg)
	}
	return err, nil
}

// note: the cleanup below leaves the shared world as it was. The owner
// unregisters without a permit, so the revoke does not block it.

func unregister(ctx *astral.Context, idx *indexingcli.Client, nonce astral.Nonce) error {
	if err := idx.UnregisterIndexer(ctx, nonce); err != nil {
		return fmt.Errorf("cleanup: unregistering the indexer: %w", err)
	}
	return nil
}

func disableRepo(ctx *astral.Context, user *astrald.Client, name string) error {
	if err := indexingcli.New(nil, user).DisableRepo(ctx, name); err != nil {
		return fmt.Errorf("cleanup: disabling indexing: %w", err)
	}
	return nil
}

func removeRepo(ctx *astral.Context, user *astrald.Client, name string) error {
	if err := objectscli.New(nil, user).RemoveRepository(ctx, name); err != nil {
		return fmt.Errorf("cleanup: removing the repository: %w", err)
	}
	return nil
}
