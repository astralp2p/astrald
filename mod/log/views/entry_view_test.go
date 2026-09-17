package views

import (
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func setHideOrigin(t *testing.T, id *astral.Identity) {
	t.Helper()
	saved := HideOrigin
	t.Cleanup(func() { HideOrigin = saved })
	HideOrigin = id
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
		{"nothing hidden", nil, other, true},
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
