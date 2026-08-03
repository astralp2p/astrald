package services

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

func recv(t *testing.T, ch <-chan *services.Update) (*services.Update, bool) {
	t.Helper()
	select {
	case update, ok := <-ch:
		return update, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an update")
		return nil, false
	}
}

func TestSnapshotHoldsWhatWasAdvertised(t *testing.T) {
	e := newExternalServices()
	provider := astral.GenerateIdentity()
	e.advertise(provider, "contacts", nil)

	ch, err := e.DiscoverServices(astral.NewContext(nil), astral.GenerateIdentity(), false)
	if err != nil {
		t.Fatal(err)
	}

	update, ok := recv(t, ch)
	if !ok || update == nil {
		t.Fatal("expected one advertisement")
	}
	if !update.Available || string(update.Name) != "contacts" {
		t.Fatalf("unexpected advertisement: %+v", update)
	}
	if !update.ProviderID.IsEqual(provider) {
		t.Fatal("advertisement names the wrong provider")
	}

	// a snapshot ends rather than waiting
	if _, ok := recv(t, ch); ok {
		t.Fatal("expected the channel to close after the snapshot")
	}
}

func TestFollowSeparatesSnapshotFromWhatFollows(t *testing.T) {
	e := newExternalServices()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	e.advertise(astral.GenerateIdentity(), "contacts", nil)

	ch, err := e.DiscoverServices(ctx, astral.GenerateIdentity(), true)
	if err != nil {
		t.Fatal(err)
	}

	if update, _ := recv(t, ch); update == nil {
		t.Fatal("expected the snapshot before the separator")
	}
	if update, _ := recv(t, ch); update != nil {
		t.Fatal("expected a nil separator after the snapshot")
	}

	later := astral.GenerateIdentity()
	e.advertise(later, "player", nil)

	update, _ := recv(t, ch)
	if update == nil || string(update.Name) != "player" {
		t.Fatalf("expected the later advertisement, got %+v", update)
	}
}

func TestChangedInfoKeepsTheServiceAvailable(t *testing.T) {
	e := newExternalServices()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	provider := astral.GenerateIdentity()
	e.advertise(provider, "contacts", nil)

	ch, err := e.DiscoverServices(ctx, astral.GenerateIdentity(), true)
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch) // snapshot
	recv(t, ch) // separator

	e.advertise(provider, "contacts", &astral.Bundle{})

	update, _ := recv(t, ch)
	if update == nil {
		t.Fatal("expected an update after the info changed")
	}
	if !update.Available {
		t.Fatal("a changed info withdrew the service; it should stay available")
	}
	if update.Info == nil {
		t.Fatal("the update carries the old info")
	}

	// and it replaced rather than accumulated
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.ads) != 1 {
		t.Fatalf("expected one advertisement, found %d", len(e.ads))
	}
}

func TestWithdrawSaysSoOnce(t *testing.T) {
	e := newExternalServices()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	provider := astral.GenerateIdentity()
	e.advertise(provider, "contacts", nil)

	ch, err := e.DiscoverServices(ctx, astral.GenerateIdentity(), true)
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch) // snapshot
	recv(t, ch) // separator

	e.withdraw(provider, "contacts")

	update, _ := recv(t, ch)
	if update == nil || update.Available {
		t.Fatalf("expected an unavailable update, got %+v", update)
	}

	// withdrawing what is already gone tells nobody anything
	e.withdraw(provider, "contacts")
	select {
	case update := <-ch:
		t.Fatalf("a second withdrawal spoke: %+v", update)
	case <-time.After(100 * time.Millisecond):
	}
}
