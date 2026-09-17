package objects

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

type removalRecorder struct {
	*mem.Repository
	removed []string
}

func (r *removalRecorder) AfterRemoved(name string) {
	r.removed = append(r.removed, name)
}

// note: the group lists m1 before m2, so Create and a sequential Read try m1 first.
func newTestGroup(t *testing.T, concurrent bool) (*Module, *RepoGroup, *mem.Repository, *mem.Repository) {
	t.Helper()

	mod := &Module{}
	m1 := mem.New("m1", 0)
	m2 := mem.New("m2", 0)
	mod.repos.Set("m1", m1)
	mod.repos.Set("m2", m2)

	group := NewRepoGroup(mod, "group", concurrent)
	mod.repos.Set("g", group)
	for _, name := range []string{"m1", "m2"} {
		if err := group.Add(name); err != nil {
			t.Fatalf("Add(%q): %v", name, err)
		}
	}

	return mod, group, m1, m2
}

func commitTo(t *testing.T, repo objectsmod.Repository, data string) *astral.ObjectID {
	t.Helper()

	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("%v.Create: %v", repo, err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("%v.Write(%q): %v", repo, data, err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("%v.Commit: %v", repo, err)
	}
	return id
}

func unknownObjectID(t *testing.T) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte("stored nowhere")))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return id
}

func readString(t *testing.T, r io.Reader) string {
	t.Helper()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func receiveGroupID(t *testing.T, ch <-chan *astral.ObjectID) (*astral.ObjectID, bool) {
	t.Helper()

	select {
	case id, ok := <-ch:
		return id, ok
	case <-time.After(2 * time.Second):
		t.Fatal("scan channel sent nothing within 2s")
		return nil, false
	}
}

func TestRepoGroup_CreateEmptyGroup(t *testing.T) {
	group := NewRepoGroup(&Module{}, "empty", false)

	_, err := group.Create(astral.NewContext(nil), nil)
	if err == nil || err.Error() != "repository group empty" {
		t.Fatalf("Create on an empty group: got %v, want \"repository group empty\"", err)
	}
}

func TestRepoGroup_CreateWritesToFirstMember(t *testing.T) {
	_, group, m1, m2 := newTestGroup(t, false)
	ctx := astral.NewContext(nil)

	id := commitTo(t, group, "hello")

	if has, _ := m1.Contains(ctx, id); !has {
		t.Errorf("m1.Contains after group commit: got false, want true")
	}
	if has, _ := m2.Contains(ctx, id); has {
		t.Errorf("m2.Contains after group commit: got true, want false")
	}
}

func TestRepoGroup_ContainsAndDelete(t *testing.T) {
	_, group, _, m2 := newTestGroup(t, false)
	ctx := astral.NewContext(nil)
	id := commitTo(t, m2, "in m2")
	unknown := unknownObjectID(t)

	if has, err := group.Contains(ctx, id); err != nil || !has {
		t.Errorf("Contains(id in m2): got %v, %v, want true, nil", has, err)
	}
	if has, err := group.Contains(ctx, unknown); err != nil || has {
		t.Errorf("Contains(unknown): got %v, %v, want false, nil", has, err)
	}

	if err := group.Delete(ctx, unknown); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Delete(unknown): got %v, want %v", err, objectsmod.ErrNotFound)
	}
	if err := group.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(id in m2): got %v, want nil", err)
	}
	if has, _ := m2.Contains(ctx, id); has {
		t.Errorf("m2.Contains after group Delete: got true, want false")
	}
}

func TestRepoGroup_Read(t *testing.T) {
	for _, tc := range []struct {
		name       string
		concurrent bool
	}{
		{"sequential", false},
		{"concurrent", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, group, _, m2 := newTestGroup(t, tc.concurrent)
			ctx := astral.NewContext(nil)
			id := commitTo(t, m2, "in m2")

			r, err := group.Read(ctx, id, 0, 0)
			if err != nil {
				t.Fatalf("Read(id in m2): %v", err)
			}
			defer r.Close()
			if got, want := readString(t, r), "in m2"; got != want {
				t.Errorf("Read(id in m2): got %q, want %q", got, want)
			}

			if _, err := group.Read(ctx, unknownObjectID(t), 0, 0); !errors.Is(err, objectsmod.ErrNotFound) {
				t.Errorf("Read(unknown): got %v, want %v", err, objectsmod.ErrNotFound)
			}
		})
	}
}

func TestRepoGroup_FreeSumsMembers(t *testing.T) {
	_, group, _, _ := newTestGroup(t, false)
	commitTo(t, group, "abc")

	free, err := group.Free(astral.NewContext(nil))
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if want := int64(2*mem.DefaultSize - 3); free != want {
		t.Fatalf("Free: got %d, want %d", free, want)
	}
}

func TestRepoGroup_AddMissingRepository(t *testing.T) {
	group := NewRepoGroup(&Module{}, "group", false)

	err := group.Add("missing")
	if err == nil || err.Error() != "repository missing not found" {
		t.Fatalf("Add(\"missing\"): got %v, want \"repository missing not found\"", err)
	}
}

func TestRepoGroup_ScanSnapshot(t *testing.T) {
	_, group, m1, m2 := newTestGroup(t, false)
	a := commitTo(t, m1, "a")
	b := commitTo(t, m2, "b")

	ch, err := group.Scan(astral.NewContext(nil), false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := map[astral.ObjectID]int{}
	for {
		id, ok := receiveGroupID(t, ch)
		if !ok {
			break
		}
		if id == nil {
			t.Fatal("Scan(follow=false) sent a nil id")
		}
		got[*id]++
	}

	if len(got) != 2 || got[*a] != 1 || got[*b] != 1 {
		t.Fatalf("Scan(follow=false): got %v, want %v and %v once each", got, a, b)
	}
}

func TestRepoGroup_ScanFollow(t *testing.T) {
	_, group, m1, m2 := newTestGroup(t, false)
	a := commitTo(t, m1, "a")
	b := commitTo(t, m2, "b")

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	ch, err := group.Scan(ctx, true)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := map[astral.ObjectID]int{}
	for {
		id, ok := receiveGroupID(t, ch)
		if !ok {
			t.Fatal("Scan(follow=true) closed before the nil boundary")
		}
		if id == nil {
			break
		}
		got[*id]++
	}
	if len(got) != 2 || got[*a] != 1 || got[*b] != 1 {
		t.Fatalf("Scan(follow=true) snapshot: got %v, want %v and %v once each", got, a, b)
	}

	c := commitTo(t, m2, "c")
	id, ok := receiveGroupID(t, ch)
	if !ok || id == nil || *id != *c {
		t.Fatalf("Scan(follow=true) after the boundary: got %v (open %v), want %v", id, ok, c)
	}

	cancel()
	for {
		if _, ok := receiveGroupID(t, ch); !ok {
			break
		}
	}
}

func TestModule_RemoveRepository(t *testing.T) {
	mod := &Module{}
	recorder := &removalRecorder{Repository: mem.New("m2", 0)}
	mod.repos.Set("m2", recorder)

	if err := mod.RemoveRepository("m2"); err != nil {
		t.Fatalf("RemoveRepository(\"m2\"): %v", err)
	}
	if !slices.Equal(recorder.removed, []string{"m2"}) {
		t.Errorf("AfterRemoved calls: got %v, want [m2]", recorder.removed)
	}
	if mod.GetRepository("m2") != nil {
		t.Errorf("GetRepository(\"m2\") after removal: got a repository, want nil")
	}

	for _, tc := range []struct {
		name string
		want string
	}{
		{"", "name is empty"},
		{"m2", "repository m2 not found"},
	} {
		err := mod.RemoveRepository(tc.name)
		if err == nil || err.Error() != tc.want {
			t.Errorf("RemoveRepository(%q): got %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestModule_AddAndRemoveGroup(t *testing.T) {
	mod, group, _, _ := newTestGroup(t, false)

	if err := mod.RemoveGroup("g", "m2"); err != nil {
		t.Fatalf("RemoveGroup(\"g\", \"m2\"): %v", err)
	}
	if got := group.List(); !slices.Equal(got, []string{"m1"}) {
		t.Errorf("List after RemoveGroup: got %v, want [m1]", got)
	}

	if err := mod.AddGroup("g", "m2"); err != nil {
		t.Fatalf("AddGroup(\"g\", \"m2\"): %v", err)
	}
	if got := group.List(); !slices.Equal(got, []string{"m1", "m2"}) {
		t.Errorf("List after AddGroup: got %v, want [m1 m2]", got)
	}

	for _, tc := range []struct {
		op   string
		fn   func(string, string) error
		repo string
		want string
	}{
		{"AddGroup", mod.AddGroup, "m1", "repo m1 is not a group"},
		{"AddGroup", mod.AddGroup, "nope", "repo nope not found"},
		{"RemoveGroup", mod.RemoveGroup, "m1", "repo m1 is not a group"},
		{"RemoveGroup", mod.RemoveGroup, "nope", "repo nope not found"},
	} {
		err := tc.fn(tc.repo, "m2")
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s(%q, \"m2\"): got %v, want %q", tc.op, tc.repo, err, tc.want)
		}
	}
}
