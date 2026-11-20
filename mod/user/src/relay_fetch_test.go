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

// drain runs the sequence to its end and reads the completeness report.
func drain(f *receiverFixture, ctx *astral.Context, siblings []*astral.Identity, issuer *astral.Identity, ask askRelayFunc) (complete bool) {
	seq, report := f.mod.fetchRelayContractsReport(ctx, siblings, issuer, ask)
	for range seq {
	}
	return report()
}

// TestFetchRelayContractsReportsIncompleteOnAnError: with two siblings, one ask
// returning an error and one returning nothing, the fetch reports incomplete;
// with both returning nothing without error, it reports complete.
func TestFetchRelayContractsReportsIncompleteOnAnError(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	failing, quiet := f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()

	ask := func(_ *astral.Context, sibling, _ *astral.Identity) ([]*auth.SignedContract, error) {
		if sibling.IsEqual(failing) {
			return nil, errors.New("link closed")
		}
		return nil, nil
	}
	if drain(f, fetchCtx(), []*astral.Identity{failing, quiet}, app, ask) {
		t.Error("a failed ask reported complete")
	}

	if !drain(f, fetchCtx(), []*astral.Identity{failing, quiet}, app, answers(nil)) {
		t.Error("two clean empty answers reported incomplete")
	}
}

// TestFetchRelayContractsReportsIncompleteWhenCtxEnds: a sibling that has not
// answered when ctx ends leaves the fetch incomplete, although the other
// sibling answered cleanly.
func TestFetchRelayContractsReportsIncompleteWhenCtxEnds(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	silent, quiet := f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()

	ask := func(ctx *astral.Context, sibling, _ *astral.Identity) ([]*auth.SignedContract, error) {
		if sibling.IsEqual(silent) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, nil
	}
	ctx, cancel := fetchCtx().WithTimeout(100 * time.Millisecond)
	defer cancel()
	if drain(f, ctx, []*astral.Identity{silent, quiet}, app, ask) {
		t.Error("a fetch cut off by ctx reported complete")
	}

	// an ask that ignores ctx and returns no error after it ended is not a clean answer
	late := func(ctx *astral.Context, _, _ *astral.Identity) ([]*auth.SignedContract, error) {
		<-ctx.Done()
		return nil, nil
	}
	ctx, cancel = fetchCtx().WithTimeout(50 * time.Millisecond)
	defer cancel()
	if drain(f, ctx, []*astral.Identity{silent}, app, late) {
		t.Error("an answer that came after ctx ended reported complete")
	}
}

// TestFetchRelayContractsReportEdges: no linked sibling is a real no
// (complete), a consumer that stops early and a run outside the network zone
// are incomplete.
func TestFetchRelayContractsReportEdges(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	sib, host := f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()

	if !drain(f, fetchCtx(), nil, app, answers(nil)) {
		t.Error("no sibling reported incomplete; want a real no")
	}

	seq, report := f.mod.fetchRelayContractsReport(fetchCtx(), []*astral.Identity{sib}, app,
		answers(map[string][]*auth.SignedContract{sib.String(): {relayForContract(app, host), relayForContract(app, host)}}))
	for range seq {
		break
	}
	if report() {
		t.Error("a consumer that stopped early reported complete")
	}

	local := astral.NewContext(nil).ExcludeZone(astral.ZoneNetwork)
	if drain(f, local, []*astral.Identity{sib}, app, answers(nil)) {
		t.Error("a run outside the network zone reported complete")
	}
}
