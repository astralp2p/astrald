package objects

import (
	"slices"
	"testing"

	"github.com/astralp2p/astrald/mod/objects/mem"
)

// TestModule_RemoveRepository_DropsFromGroups pins the second clause of the
// RemoveRepository doc comment: a removed repository leaves every group listing it.
func TestModule_RemoveRepository_DropsFromGroups(t *testing.T) {
	mod := &Module{}
	mod.repos.Set("m1", mem.New("m1", 0))
	mod.repos.Set("m2", mem.New("m2", 0))

	group := NewRepoGroup(mod, "group", false)
	mod.repos.Set("g", group)
	for _, name := range []string{"m1", "m2"} {
		if err := group.Add(name); err != nil {
			t.Fatalf("Add(%q): %v", name, err)
		}
	}

	if err := mod.RemoveRepository("m2"); err != nil {
		t.Fatalf("RemoveRepository(%q): %v", "m2", err)
	}

	if got := group.List(); !slices.Equal(got, []string{"m1"}) {
		t.Errorf("group List after RemoveRepository(%q): got %v, want [m1]", "m2", got)
	}
}
