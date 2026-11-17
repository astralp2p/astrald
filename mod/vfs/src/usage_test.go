package vfs

import (
	"math/rand/v2"
	"testing"
)

// TestStoredUsageMatchesTheRecount drives random changes through a VFS and checks after each one that the stored
// usage equals the usage computed from the entries.
func TestStoredUsageMatchesTheRecount(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	rng := rand.New(rand.NewPCG(1, 2))

	names := []string{"a", "b", "c"}
	contents := []string{"x", "yy", "zzz", "wwww"}
	path := func() []string {
		p := make([]string, 1+rng.IntN(3))
		for i := range p {
			p[i] = names[rng.IntN(len(names))]
		}
		return p
	}

	for step := range 2000 {
		var op string
		switch rng.IntN(4) {
		case 0:
			op = "mkdir"
			_ = mod.changeVFS(vol, func(c *change) error { return c.createFolder(path()) })
		case 1:
			op = "write"
			_, _ = mod.recordFile(vol, path(), contentID(contents[rng.IntN(len(contents))]))
		case 2:
			op = "move"
			_ = mod.changeVFS(vol, func(c *change) error {
				_, err := c.moveEntry(path(), path())
				return err
			})
		case 3:
			op = "remove"
			_ = mod.changeVFS(vol, func(c *change) error { return c.removeEntry(path(), rng.IntN(2) == 0) })
		}

		stored, err := mod.db.measureUsage(vol.Identity)
		if err != nil {
			t.Fatal(err)
		}
		if want := recountUsage(t, mod, vol.Identity); stored != want {
			t.Fatalf("step %v (%v): stored usage %+v, recount %+v", step, op, stored, want)
		}
	}
}

// TestRemoveKeepsContentAnotherFileHolds checks that removing one of two files with the same content keeps its
// bytes counted, and removing the last one uncounts them.
func TestRemoveKeepsContentAnotherFileHolds(t *testing.T) {
	mod, vol, _ := changeTestModule(t)

	for _, p := range [][]string{{"d", "a"}, {"b"}} {
		if _, err := mod.recordFile(vol, p, contentID("12345")); err != nil {
			t.Fatal(err)
		}
	}

	steps := []struct {
		names []string
		want  vfsUsage
	}{
		{[]string{"d"}, vfsUsage{Bytes: 5, Inodes: 1}},
		{[]string{"b"}, vfsUsage{Bytes: 0, Inodes: 0}},
	}
	for _, s := range steps {
		if err := mod.changeVFS(vol, func(c *change) error { return c.removeEntry(s.names, true) }); err != nil {
			t.Fatal(err)
		}
		if u, _ := mod.db.measureUsage(vol.Identity); u != s.want {
			t.Errorf("after removing %v: usage %+v, want %+v", s.names, u, s.want)
		}
	}
}
