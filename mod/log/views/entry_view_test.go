package views

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestShowOriginHidesByDefault(t *testing.T) {
	HideOrigin.Set(nil)

	if showOrigin(astral.GenerateIdentity()) {
		t.Error("an unset HideOrigin shows an origin, want hidden")
	}
}

func TestShowOrigin(t *testing.T) {
	defer HideOrigin.Set(nil)

	var node = astral.GenerateIdentity()
	var other = astral.GenerateIdentity()

	HideOrigin.Set(node)

	if showOrigin(node) {
		t.Error("the hidden origin shows, want hidden")
	}
	if !showOrigin(other) {
		t.Error("another origin is hidden, want shown")
	}

	HideOrigin.Set(astral.Anyone)

	if showOrigin(other) {
		t.Error("a zero HideOrigin shows an origin, want hidden")
	}
}

// TestHideOriginRace renders entries while HideOrigin is set, which is the
// shape of module load: earlier-loaded modules log from their own goroutines
// while mod/log sets the origin. Run under -race.
func TestHideOriginRace(t *testing.T) {
	defer HideOrigin.Set(nil)

	var ids = [4]*astral.Identity{
		astral.GenerateIdentity(),
		astral.GenerateIdentity(),
		astral.GenerateIdentity(),
		astral.Anyone,
	}

	var view = EntryView{log.NewEntry(ids[0], 0, astral.NewString32("entry"))}

	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				_ = view.Render()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			HideOrigin.Set(ids[i%len(ids)])
		}
	}()

	wg.Wait()
}

func setHideOrigin(t *testing.T, id *astral.Identity) {
	t.Helper()
	t.Cleanup(func() { HideOrigin.Set(nil) })
	HideOrigin.Set(id)
}

func renderEntry(origin *astral.Identity) string {
	entry := &log.Entry{
		Origin:  origin,
		Level:   1,
		Time:    astral.Time(time.Date(2024, 3, 5, 12, 34, 56, 789e6, time.UTC)),
		Objects: []astral.Object{astral.NewString8("hi")},
	}
	return ansi.ReplaceAllString(EntryView{Entry: entry}.Render(), "")
}

func TestEntryViewHidesOnlyTheHiddenOrigin(t *testing.T) {
	node, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	for _, c := range []struct {
		name       string
		hide       *astral.Identity
		origin     *astral.Identity
		wantPrefix bool
	}{
		{"unset hides every origin", nil, other, false},
		{"anyone hides every origin", astral.Anyone, other, false},
		{"a foreign origin", node, other, true},
		{"the hidden origin", other, other, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			setHideOrigin(t, c.hide)

			got := renderEntry(c.origin)

			if !strings.HasSuffix(got, "hi") {
				t.Errorf("render = %q, want it to end with %q", got, "hi")
			}
			if c.wantPrefix {
				if !strings.HasPrefix(got, "[") || !strings.Contains(got, "] (1) ") {
					t.Errorf("render = %q, want an origin prefix before %q", got, "(1) ")
				}
				return
			}
			if !strings.HasPrefix(got, "(1) ") {
				t.Errorf("render = %q, want it to start with %q and no origin", got, "(1) ")
			}
		})
	}
}
