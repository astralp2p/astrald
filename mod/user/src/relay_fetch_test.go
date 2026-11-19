package user

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

func fetchCtx() *astral.Context {
	return astral.NewContext(nil).IncludeZone(astral.ZoneNetwork)
}

// fetched collects what fetchRelayContracts yields, per sender.
func fetched(f *receiverFixture, ctx *astral.Context, siblings []*astral.Identity, issuer *astral.Identity, ask askRelayFunc) map[string][]*auth.SignedContract {
	got := map[string][]*auth.SignedContract{}
	for sender, sc := range f.mod.fetchRelayContracts(ctx, siblings, issuer, ask) {
		got[sender.String()] = append(got[sender.String()], sc)
	}
	return got
}

// answers returns an ask in which each sibling answers what by holds for it.
func answers(by map[string][]*auth.SignedContract) askRelayFunc {
	return func(_ *astral.Context, sibling, _ *astral.Identity) ([]*auth.SignedContract, error) {
		return by[sibling.String()], nil
	}
}

// TestFetchRelayContractsYieldsPerSibling: every sibling is asked, and each
// contract it answers that a node admits from it is yielded with it.
func TestFetchRelayContractsYieldsPerSibling(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	a, b, host := f.addSibling(), f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()
	fromA, fromB := relayForContract(app, host), relayForContract(app, a)

	got := fetched(f, fetchCtx(), []*astral.Identity{a, b}, app, answers(map[string][]*auth.SignedContract{
		a.String(): {fromA},
		b.String(): {fromB},
	}))

	if len(got[a.String()]) != 1 || got[a.String()][0] != fromA {
		t.Errorf("from %v got %v; want its contract", a, got[a.String()])
	}
	if len(got[b.String()]) != 1 || got[b.String()][0] != fromB {
		t.Errorf("from %v got %v; want its contract", b, got[b.String()])
	}
}

// TestFetchRelayContractsFiltersWhatANodeRefuses: a contract of another issuer,
// one with another permit beside relay-for, one with no relay-for permit, one
// whose subject is outside the swarm, and any contract a sender outside the
// swarm delivers are never yielded.
func TestFetchRelayContractsFiltersWhatANodeRefuses(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	member, host := f.addSibling(), f.addSibling()
	stranger := astral.GenerateIdentity()
	app := astral.GenerateIdentity()

	got := fetched(f, fetchCtx(), []*astral.Identity{member, stranger}, app, answers(map[string][]*auth.SignedContract{
		member.String(): {
			relayForContract(astral.GenerateIdentity(), host),
			contractWith(app, host, nodes.RelayForAction{}.ObjectType(), "mod.objects.read_action"),
			contractWith(app, host, "mod.objects.read_action"),
			relayForContract(app, astral.GenerateIdentity()),
			nil,
		},
		stranger.String(): {relayForContract(app, host)},
	}))

	if len(got) != 0 {
		t.Fatalf("yielded %v; want nothing", got)
	}
}

// TestFetchRelayContractsKeepsWhatAFailedAskCollected: a sibling whose answer
// ends in an error still yields the contracts it answered before it.
func TestFetchRelayContractsKeepsWhatAFailedAskCollected(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	sib, host := f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()
	sc := relayForContract(app, host)

	got := fetched(f, fetchCtx(), []*astral.Identity{sib}, app,
		func(*astral.Context, *astral.Identity, *astral.Identity) ([]*auth.SignedContract, error) {
			return []*auth.SignedContract{sc}, errors.New("link closed")
		})

	if len(got[sib.String()]) != 1 {
		t.Fatalf("yielded %v; want the one contract", got)
	}
}

// TestFetchRelayContractsHonoursCtx: a silent sibling holds the fetch no longer
// than ctx, a consumer that stops ends the asks still running, and outside the
// network zone nothing is asked.
func TestFetchRelayContractsHonoursCtx(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	silent, answering, host := f.addSibling(), f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()

	ended := make(chan struct{}, 2)
	ask := func(ctx *astral.Context, sibling, _ *astral.Identity) ([]*auth.SignedContract, error) {
		if sibling.IsEqual(answering) {
			return []*auth.SignedContract{relayForContract(app, host), relayForContract(app, host)}, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		ended <- struct{}{}
		return nil, ctx.Err()
	}

	ctx, cancel := fetchCtx().WithTimeout(200 * time.Millisecond)
	defer cancel()
	start := time.Now()
	got := fetched(f, ctx, []*astral.Identity{silent, answering}, app, ask)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("the fetch took %v; want it ended with ctx", d)
	}
	if len(got[answering.String()]) != 2 {
		t.Fatalf("yielded %v; want the answering sibling's contracts", got)
	}
	<-ended

	for range f.mod.fetchRelayContracts(fetchCtx(), []*astral.Identity{answering, silent}, app, ask) {
		break
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the silent ask kept running after the consumer stopped")
	}

	asked := false
	local := astral.NewContext(nil).ExcludeZone(astral.ZoneNetwork)
	for range f.mod.fetchRelayContracts(local, []*astral.Identity{answering}, app,
		func(*astral.Context, *astral.Identity, *astral.Identity) ([]*auth.SignedContract, error) {
			asked = true
			return nil, nil
		}) {
	}
	if asked {
		t.Fatal("asked a sibling outside the network zone")
	}
}
