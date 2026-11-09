package fs

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// attachAuth answers every action with verdict and keeps each action it was asked about.
//
// note: the embedded nil interface panics on any method other than Authorize.
// note: read only after the op returned (routeAttach waits on LogFunc), so no lock is needed.
type attachAuth struct {
	authmod.Module
	verdict bool
	actions []auth.ActionObject
}

func (a *attachAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.actions = append(a.actions, action)
	return a.verdict
}

// attachObjects keeps every repository registered with it and answers nothing else.
//
// note: the embedded nil interface panics on any method other than AddRepository and AddGroup.
// note: read only after the op returned (routeAttach waits on LogFunc), so no lock is needed.
type attachObjects struct {
	objectsmod.Module
	repos map[string]objectsmod.Repository
}

func (o *attachObjects) AddRepository(name string, repo objectsmod.Repository) error {
	if o.repos == nil {
		o.repos = map[string]objectsmod.Repository{}
	}
	o.repos[name] = repo
	return nil
}

func (o *attachObjects) AddGroup(string, string) error { return nil }

func (o *attachObjects) names() []string {
	var names []string
	for name := range o.repos {
		names = append(names, name)
	}
	return names
}

// answerBuffer is the caller's end of the connection; it keeps what the op writes.
type answerBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *answerBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *answerBuffer) Close() error { return nil }

func (w *answerBuffer) objects(t *testing.T) []astral.Object {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()

	var objs []astral.Object
	recv := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	for {
		obj, err := recv.Receive()
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		objs = append(objs, obj)
	}
}

// routeAttach sends one query for op to fn from a fresh caller with origin, naming a fresh
// temporary directory, and answers the single object the op wrote once it returned.
func routeAttach(t *testing.T, fn any, op, origin string) astral.Object {
	t.Helper()

	o, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	returned := make(chan struct{}, 1)
	o.LogFunc = func(*routing.Report) { returned <- struct{}{} }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	caller := astral.GenerateIdentity()
	args := map[string]any{"path": t.TempDir(), "name": "attached", "temporary": "true"}
	q := astral.Launch(query.New(caller, caller, op, args))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	w := &answerBuffer{}
	requester, err := o.RouteQuery(ctx, q, w)
	if err != nil {
		t.Fatalf("route %v: %v", op, err)
	}
	requester.Close()

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatalf("%v never returned", op)
	}

	objs := w.objects(t)
	if len(objs) != 1 {
		t.Fatalf("%v answered %v; want one object", op, objs)
	}
	return objs[0]
}

// newAttachModule is an fs module holding attachAuth and attachObjects, with the
// state fs.new_watch needs to build a watch repository and start its scan.
func newAttachModule(t *testing.T, authority *attachAuth) (*Module, *attachObjects) {
	t.Helper()

	store := &attachObjects{}
	mod := testWatchRepository(t, t.TempDir()).mod
	mod.Deps = Deps{Auth: authority, Objects: store}
	mod.log = log.New(astral.GenerateIdentity())
	mod.log.SetFilter(func(*log.Entry) bool { return false })
	mod.indexer = NewIndexer(mod)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	mod.ctx = ctx

	t.Cleanup(func() {
		for name, repo := range store.repos {
			if removed, ok := repo.(objectsmod.AfterRemovedCallback); ok {
				removed.AfterRemoved(name)
			}
		}
		cancel()
	})
	return mod, store
}

// attachOps names each op that attaches a host directory and picks its handler.
var attachOps = []struct {
	name string
	fn   func(*Module) any
}{
	{"fs.new_repo", func(mod *Module) any { return mod.OpNewRepo }},
	{"fs.new_watch", func(mod *Module) any { return mod.OpNewWatch }},
}

// TestAttachRefusesNetworkOrigin: a query from the network to fs.new_repo or
// fs.new_watch is answered origin not allowed before any authorization, and no
// repository is registered.
func TestAttachRefusesNetworkOrigin(t *testing.T) {
	for _, op := range attachOps {
		t.Run(op.name, func(t *testing.T) {
			authority := &attachAuth{verdict: true}
			mod, store := newAttachModule(t, authority)

			answer := routeAttach(t, op.fn(mod), op.name, astral.OriginNetwork)

			msg, ok := answer.(*astral.ErrorMessage)
			if !ok {
				t.Fatalf("answered %v; want an error_message", answer)
			}
			if got, want := msg.Error(), astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}
			if n := len(authority.actions); n != 0 {
				t.Fatalf("made %d authorization calls; want none", n)
			}
			if names := store.names(); len(names) != 0 {
				t.Fatalf("registered %v; want no repository", names)
			}
		})
	}
}

// TestAttachLocalWithPermit: a local caller holding AdminObjects still attaches the
// directory: the op asks AdminObjects for the caller, answers ack and registers the
// repository.
func TestAttachLocalWithPermit(t *testing.T) {
	for _, op := range attachOps {
		t.Run(op.name, func(t *testing.T) {
			authority := &attachAuth{verdict: true}
			mod, store := newAttachModule(t, authority)

			answer := routeAttach(t, op.fn(mod), op.name, "")

			if _, ok := answer.(*astral.Ack); !ok {
				t.Fatalf("answered %v; want an ack", answer)
			}
			actions := authority.actions
			if len(actions) != 1 {
				t.Fatalf("made %d authorization calls; want 1", len(actions))
			}
			if _, ok := actions[0].(*auth.AdminObjectsAction); !ok {
				t.Fatalf("named %q; want %q", actions[0].ObjectType(), auth.AdminObjectsAction{}.ObjectType())
			}
			if names := store.names(); len(names) != 1 || names[0] != "attached" {
				t.Fatalf("registered %v; want [attached]", names)
			}
		})
	}
}
