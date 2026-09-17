package services

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

type preparedDiscoverer struct {
	ch  chan *services.Update
	err error
}

func (d *preparedDiscoverer) DiscoverServices(*astral.Context, *astral.Identity, bool) (<-chan *services.Update, error) {
	return d.ch, d.err
}

func discoveryModule(t *testing.T, discoverers ...*preparedDiscoverer) *Module {
	t.Helper()
	l := log.New(astral.GenerateIdentity())
	l.SetFilter(func(*log.Entry) bool { return false })

	mod := &Module{log: l}
	for _, d := range discoverers {
		if err := mod.AddDiscoverer(d); err != nil {
			t.Fatalf("AddDiscoverer: %v", err)
		}
	}
	return mod
}

func closedSource(names ...string) chan *services.Update {
	ch := make(chan *services.Update, len(names))
	for _, name := range names {
		ch <- &services.Update{Available: true, Name: astral.String8(name)}
	}
	close(ch)
	return ch
}

func TestDiscoverServicesMergesEverySnapshot(t *testing.T) {
	mod := discoveryModule(t,
		&preparedDiscoverer{ch: closedSource("a")},
		&preparedDiscoverer{ch: closedSource("b")},
		&preparedDiscoverer{err: errors.New("discovery failed")},
	)

	out, err := mod.DiscoverServices(astral.NewContext(nil), astral.GenerateIdentity(), false)
	if err != nil {
		t.Fatalf("DiscoverServices error = %v, want nil", err)
	}

	got := map[string]int{}
	for {
		update, ok := recv(t, out)
		if !ok {
			break
		}
		if update == nil {
			t.Fatal("a snapshot-only discovery sent the nil separator")
		}
		got[string(update.Name)]++
	}

	if len(got) != 2 || got["a"] != 1 || got["b"] != 1 {
		t.Fatalf("merged snapshot = %v, want a and b once each", got)
	}
}

func TestDiscoverServicesFollowSeparatesSnapshotFromLive(t *testing.T) {
	source := make(chan *services.Update, 3)
	source <- &services.Update{Available: true, Name: "c1"}
	source <- nil
	mod := discoveryModule(t, &preparedDiscoverer{ch: source})

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	out, err := mod.DiscoverServices(ctx, astral.GenerateIdentity(), true)
	if err != nil {
		t.Fatalf("DiscoverServices error = %v, want nil", err)
	}

	if update, ok := recv(t, out); !ok || update == nil || update.Name != "c1" {
		t.Fatalf("first update = %+v (open %v), want c1", update, ok)
	}
	if update, ok := recv(t, out); !ok || update != nil {
		t.Fatalf("second update = %+v (open %v), want the nil separator", update, ok)
	}

	source <- &services.Update{Available: true, Name: "c2"}
	if update, ok := recv(t, out); !ok || update == nil || update.Name != "c2" {
		t.Fatalf("live update = %+v (open %v), want c2", update, ok)
	}

	cancel()
	close(source)
	if update, ok := recv(t, out); ok {
		t.Fatalf("got %+v after cancel and close, want the output closed", update)
	}
}

func TestDiscoverServicesWithNoDiscoverers(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		out, err := discoveryModule(t).DiscoverServices(astral.NewContext(nil), astral.GenerateIdentity(), false)
		if err != nil {
			t.Fatalf("DiscoverServices error = %v, want nil", err)
		}
		if update, ok := recv(t, out); ok {
			t.Fatalf("got %+v, want the output closed", update)
		}
	})

	t.Run("follow", func(t *testing.T) {
		out, err := discoveryModule(t).DiscoverServices(astral.NewContext(nil), astral.GenerateIdentity(), true)
		if err != nil {
			t.Fatalf("DiscoverServices error = %v, want nil", err)
		}
		if update, ok := recv(t, out); !ok || update != nil {
			t.Fatalf("first update = %+v (open %v), want the nil separator", update, ok)
		}
		if update, ok := recv(t, out); ok {
			t.Fatalf("got %+v after the separator, want the output closed", update)
		}
	})
}
