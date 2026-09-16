package objects

import (
	"bufio"
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

// bufferWriter keeps what the op answered so the stream can be decoded.
type bufferWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func (w *bufferWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *bufferWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

type jsonEnvelope struct {
	Type   string
	Object json.RawMessage
}

// readRepositories answers objects.repositories in JSON and returns every
// repository_info by name and the type of the last streamed object.
func readRepositories(t *testing.T, mod *Module) (map[string]objects.RepositoryInfo, string) {
	t.Helper()

	w := &bufferWriter{closed: make(chan struct{})}
	if err := route(t, mod.OpRepositories, astral.GenerateIdentity(), "objects.repositories?out=json", w); err != nil {
		t.Fatalf("route: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("objects.repositories did not finish")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	infos := map[string]objects.RepositoryInfo{}
	var last string

	scanner := bufio.NewScanner(&w.buf)
	for scanner.Scan() {
		var env jsonEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			t.Fatalf("decode %s: %v", scanner.Bytes(), err)
		}
		last = env.Type

		if env.Type != (objects.RepositoryInfo{}).ObjectType() {
			continue
		}

		var info objects.RepositoryInfo
		if err := json.Unmarshal(env.Object, &info); err != nil {
			t.Fatalf("decode %s: %v", env.Object, err)
		}
		infos[string(info.Name)] = info
	}

	return infos, last
}

func TestRepositories_ReportsGroupMembershipAndConcurrency(t *testing.T) {
	mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}, config: defaultConfig}
	mod.setupDefaultRepos()
	mod.repos.Set("empty", NewRepoGroup(mod, "Empty sequential", false))

	infos, last := readRepositories(t, mod)

	if last != (&astral.EOS{}).ObjectType() {
		t.Fatalf("stream ends with %q; want eos", last)
	}

	if len(infos) != len(mod.repos.Clone()) {
		t.Fatalf("got %d records; want one per registered repository (%d)", len(infos), len(mod.repos.Clone()))
	}

	want := map[string]struct {
		kind       string
		children   []astral.String8
		concurrent astral.Bool
	}{
		objects.RepoMain:      {objects.RepositoryKindGroup, []astral.String8{"device", "virtual", "network"}, false},
		objects.RepoDevice:    {objects.RepositoryKindGroup, []astral.String8{"memory", "local", "removable"}, false},
		objects.RepoMemory:    {objects.RepositoryKindGroup, []astral.String8{"mem0", "system"}, false},
		objects.RepoLocal:     {objects.RepositoryKindGroup, []astral.String8{}, true},
		objects.RepoRemovable: {objects.RepositoryKindGroup, []astral.String8{}, true},
		objects.RepoVirtual:   {objects.RepositoryKindGroup, []astral.String8{}, true},
		objects.RepoNetwork:   {objects.RepositoryKindGroup, []astral.String8{}, true},
		"empty":               {objects.RepositoryKindGroup, []astral.String8{}, false},
		"mem0":                {objects.RepositoryKindRepository, []astral.String8{}, false},
		objects.RepoSystem:    {objects.RepositoryKindRepository, []astral.String8{}, false},
	}

	for name, w := range want {
		info, ok := infos[name]
		if !ok {
			t.Fatalf("no record for %q", name)
		}
		if string(info.Kind) != w.kind {
			t.Errorf("%s: kind %q; want %q", name, info.Kind, w.kind)
		}
		if !reflect.DeepEqual(info.Children, w.children) {
			t.Errorf("%s: children %v; want %v", name, info.Children, w.children)
		}
		if info.Concurrent != w.concurrent {
			t.Errorf("%s: concurrent %v; want %v", name, info.Concurrent, w.concurrent)
		}
	}
}
