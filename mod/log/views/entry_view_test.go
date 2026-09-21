package views

import (
	"sync"
	"testing"

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
