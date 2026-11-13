package vfs

import (
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/lib/routing"
)

// TestEveryOperationIsRouted checks that the router mounts one route for each method of the SDK.
// note: AddStructPrefix skips a method whose signature the router cannot wrap, without an error.
func TestEveryOperationIsRouted(t *testing.T) {
	var router routing.OpRouter

	if err := router.AddStructPrefix(&Module{}, "Op"); err != nil {
		t.Fatalf("AddStructPrefix: %v", err)
	}

	methods := []string{
		vfsapi.MethodLs, vfsapi.MethodStat, vfsapi.MethodStatfs, vfsapi.MethodRead,
		vfsapi.MethodWrite, vfsapi.MethodMkdir, vfsapi.MethodMove, vfsapi.MethodRemove,
		vfsapi.MethodTouch, vfsapi.MethodVolumes, vfsapi.MethodCreate, vfsapi.MethodSetQuota,
	}

	for _, method := range methods {
		name := method[len(vfsapi.ModuleName+"."):]
		if !router.HasRoute(name) {
			t.Errorf("no route %q for %s", name, method)
		}
	}

	if got := len(router.Spec()); got != len(methods) {
		t.Errorf("%d routes, want %d", got, len(methods))
	}
}

func TestOperationArguments(t *testing.T) {
	var router routing.OpRouter
	if err := router.AddStructPrefix(&Module{}, "Op"); err != nil {
		t.Fatalf("AddStructPrefix: %v", err)
	}

	want := map[string][]string{
		"ls":        {"in", "out", "vfs", "path"},
		"stat":      {"in", "out", "vfs", "path"},
		"statfs":    {"in", "out", "vfs"},
		"read":      {"vfs", "path", "offset", "limit"},
		"write":     {"in", "out", "vfs", "path"},
		"mkdir":     {"in", "out", "vfs", "path"},
		"move":      {"in", "out", "vfs", "from", "to"},
		"remove":    {"in", "out", "vfs", "path", "recursive"},
		"touch":     {"in", "out", "vfs", "path", "time"},
		"volumes":   {"in", "out"},
		"create":    {"in", "out", "vfs"},
		"set_quota": {"in", "out", "vfs", "bytes", "inodes"},
	}

	for _, spec := range router.Spec() {
		var got []string
		for _, p := range spec.Parameters {
			got = append(got, string(p.Name))
		}

		name := spec.Name.String()
		if w, ok := want[name]; !ok {
			t.Errorf("unexpected route %q", name)
		} else if !sameSet(got, w) {
			t.Errorf("%s arguments = %v, want %v", name, got, w)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}

	return true
}
