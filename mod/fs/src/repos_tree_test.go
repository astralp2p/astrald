package fs

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// memNode is an in-memory tree.Node holding one value and named sub-nodes.
type memNode struct {
	mu     sync.Mutex
	parent *memNode
	name   string
	value  astral.Object
	subs   map[string]*memNode
}

var _ tree.Node = &memNode{}

func newMemNode(parent *memNode, name string) *memNode {
	return &memNode{parent: parent, name: name, value: &astral.Nil{}, subs: map[string]*memNode{}}
}

// note: follow is ignored; the channel carries the current value and closes.
func (n *memNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan astral.Object, 1)
	ch <- n.value
	close(ch)
	return ch, nil
}

func (n *memNode) Set(_ *astral.Context, object astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.value = object
	return nil
}

func (n *memNode) Delete(*astral.Context) error {
	n.parent.mu.Lock()
	defer n.parent.mu.Unlock()

	delete(n.parent.subs, n.name)
	return nil
}

func (n *memNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	subs := make(map[string]tree.Node, len(n.subs))
	for name, sub := range n.subs {
		subs[name] = sub
	}
	return subs, nil
}

func (n *memNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, ok := n.subs[name]; ok {
		return nil, tree.ErrAlreadyExists
	}
	sub := newMemNode(n, name)
	n.subs[name] = sub
	return sub, nil
}

// memTree serves a memNode as the tree root and answers nothing else.
type memTree struct {
	treemod.Module
	root *memNode
}

func (t *memTree) Root() tree.Node { return t.root }

// memObjects is a repository registry that fires AfterRemoved on removal, as the objects
// module does, and answers nothing else.
type memObjects struct {
	objectsmod.Module
	repos map[string]objectsmod.Repository
}

func (o *memObjects) AddRepository(name string, repo objectsmod.Repository) error {
	o.repos[name] = repo
	return nil
}

func (o *memObjects) GetRepository(name string) objectsmod.Repository { return o.repos[name] }

func (o *memObjects) AddGroup(string, string) error { return nil }

func (o *memObjects) RemoveRepository(name string) error {
	repo := o.repos[name]
	delete(o.repos, name)

	if c, ok := repo.(objectsmod.AfterRemovedCallback); ok {
		c.AfterRemoved(name)
	}

	return nil
}

func newTreeModule(t *testing.T) (*Module, *memObjects) {
	t.Helper()

	logger := log.New(astral.GenerateIdentity())
	logger.SetFilter(func(*log.Entry) bool { return false })

	objects := &memObjects{repos: map[string]objectsmod.Repository{}}
	mod := &Module{
		Deps: Deps{Objects: objects, Tree: &memTree{root: newMemNode(nil, "")}},
		log:  logger,
		ctx:  astral.NewContext(nil),
	}

	return mod, objects
}

func treeEntries(t *testing.T, mod *Module) map[string]RepoConfig {
	t.Helper()

	repos, err := mod.loadRepos(mod.ctx)
	if err != nil {
		t.Fatal(err)
	}

	return repos
}

// TestSaveRepo_RoundTrip: a saved entry reads back with every field it was saved with.
func TestSaveRepo_RoundTrip(t *testing.T) {
	mod, _ := newTreeModule(t)
	want := RepoConfig{Label: "Photos", Path: "/data/photos", Writable: true}

	if err := mod.saveRepo(mod.ctx, "photos", want); err != nil {
		t.Fatal(err)
	}

	got := treeEntries(t, mod)
	if len(got) != 1 || got["photos"] != want {
		t.Fatalf("want {photos: %v}, got %v", want, got)
	}
}

// TestSaveRepo_ReplacesEntry: saving a name twice keeps the last value.
func TestSaveRepo_ReplacesEntry(t *testing.T) {
	mod, _ := newTreeModule(t)

	mod.saveRepo(mod.ctx, "r", RepoConfig{Label: "a", Path: "/a", Writable: true})
	mod.saveRepo(mod.ctx, "r", RepoConfig{Label: "b", Path: "/b"})

	got := treeEntries(t, mod)["r"]
	if got != (RepoConfig{Label: "b", Path: "/b"}) {
		t.Fatalf("got %v", got)
	}
}

// TestSaveRepo_RejectsUnsafeName: a name with '/' or no characters is refused and writes nothing.
func TestSaveRepo_RejectsUnsafeName(t *testing.T) {
	mod, _ := newTreeModule(t)

	for _, name := range []string{"", "a/b"} {
		if err := mod.saveRepo(mod.ctx, name, RepoConfig{Path: "/a"}); err != errInvalidRepoName {
			t.Fatalf("name %q: want errInvalidRepoName, got %v", name, err)
		}
	}

	if got := treeEntries(t, mod); len(got) != 0 {
		t.Fatalf("want no entries, got %v", got)
	}
}

// TestAddTreeRepos_RestoresPersistedRepo: an entry whose directory exists is registered,
// and removing the restored repository deletes its entry.
func TestAddTreeRepos_RestoresPersistedRepo(t *testing.T) {
	mod, objects := newTreeModule(t)
	dir := t.TempDir()

	mod.saveRepo(mod.ctx, "kept", RepoConfig{Label: "Kept", Path: dir, Writable: true})

	mod.addTreeRepos(mod.ctx)

	repo, ok := objects.repos["kept"].(*Repository)
	if !ok || !repo.persisted || repo.root != dir {
		t.Fatalf("want a persisted writable repo at %v, got %#v", dir, objects.repos["kept"])
	}

	objects.RemoveRepository("kept")

	if got := treeEntries(t, mod); len(got) != 0 {
		t.Fatalf("want entry deleted with its repo, got %v", got)
	}
}

// TestAddTreeRepos_SkipsUnusablePath: an entry whose path is missing or relative is not
// registered, and its entry stays in the tree.
func TestAddTreeRepos_SkipsUnusablePath(t *testing.T) {
	mod, objects := newTreeModule(t)

	mod.saveRepo(mod.ctx, "gone", RepoConfig{Path: filepath.Join(t.TempDir(), "missing"), Writable: true})
	mod.saveRepo(mod.ctx, "relative", RepoConfig{Path: "relative", Writable: true})

	mod.addTreeRepos(mod.ctx)

	if len(objects.repos) != 0 {
		t.Fatalf("want nothing registered, got %v", objects.repos)
	}

	if got := treeEntries(t, mod); len(got) != 2 {
		t.Fatalf("want both entries kept, got %v", got)
	}
}

// TestAddTreeRepos_ConfigWinsNameClash: a name registered before the restore keeps its repo,
// and the tree entry stays.
func TestAddTreeRepos_ConfigWinsNameClash(t *testing.T) {
	mod, objects := newTreeModule(t)
	configured := NewRepository(mod, "configured", t.TempDir())
	objects.repos["clash"] = configured

	mod.saveRepo(mod.ctx, "clash", RepoConfig{Path: t.TempDir(), Writable: true})

	mod.addTreeRepos(mod.ctx)

	if objects.repos["clash"] != objectsmod.Repository(configured) {
		t.Fatalf("want the configured repo kept, got %#v", objects.repos["clash"])
	}

	if got := treeEntries(t, mod); len(got) != 1 {
		t.Fatalf("want the entry kept, got %v", got)
	}
}

// TestRemoveRepository_TemporaryRepoKeepsTree: removing a repository that was not persisted
// leaves a tree entry of the same name alone.
func TestRemoveRepository_TemporaryRepoKeepsTree(t *testing.T) {
	mod, objects := newTreeModule(t)
	mod.saveRepo(mod.ctx, "same", RepoConfig{Path: "/elsewhere", Writable: true})

	objects.AddRepository("same", NewRepository(mod, "same", t.TempDir()))
	objects.RemoveRepository("same")

	if got := treeEntries(t, mod); len(got) != 1 {
		t.Fatalf("want the entry kept, got %v", got)
	}
}

// TestPersistAdded_FailedSaveRemovesRepo: a repository whose entry cannot be saved is removed
// again, so no live repository is left unsaved.
func TestPersistAdded_FailedSaveRemovesRepo(t *testing.T) {
	mod, objects := newTreeModule(t)
	objects.AddRepository("a/b", NewRepository(mod, "a/b", t.TempDir()))

	err := mod.persistAdded(mod.ctx, "a/b", RepoConfig{Path: "/a", Writable: true})

	if err != errInvalidRepoName {
		t.Fatalf("want errInvalidRepoName, got %v", err)
	}

	if len(objects.repos) != 0 {
		t.Fatalf("want the repo removed, got %v", objects.repos)
	}
}
