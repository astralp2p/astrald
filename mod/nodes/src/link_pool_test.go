package nodes

import (
	"slices"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
)

// newOutboundLinks builds eight outbound links from localID to remoteID on mod; the
// test's cleanup ends each link.
func newOutboundLinks(t *testing.T, mod *Module, localID, remoteID *astral.Identity) []*Link {
	t.Helper()

	links := make([]*Link, 8)
	for i := range links {
		ch := channel.New(&recordingLink{}, channel.WithLockedWrites())
		links[i] = &Link{
			id:       astral.NewNonce(),
			conn:     &identityConn{remote: remoteID},
			mux:      newMux(mod, ch, localID, remoteID, nil, nil),
			done:     make(chan struct{}),
			outbound: true,
		}
	}
	t.Cleanup(func() {
		for _, l := range links {
			close(l.done)
		}
	})
	return links
}

// TestAddLinkCountsConcurrentLinksOnce: of links to one node added at once,
// exactly one reports itself as the first link, and the counts run 1 to n.
// note: the user module syncs on LinkCount 1 alone (mod/user/src/object_receiver.go),
// so a count no link reports as 1 skips the first-link sync.
func TestAddLinkCountsConcurrentLinksOnce(t *testing.T) {
	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)
	links := newOutboundLinks(t, mod, localID, remoteID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, l := range links {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := mod.linkPool.AddLink(l); err != nil {
				t.Errorf("AddLink: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	var counts []int
	for _, e := range events.recorded() {
		if created, ok := e.(*nodes.LinkCreatedEvent); ok {
			counts = append(counts, int(created.LinkCount))
		}
	}
	slices.Sort(counts)

	want := make([]int, len(links))
	for i := range want {
		want[i] = i + 1
	}
	if !slices.Equal(counts, want) {
		t.Fatalf("LinkCreatedEvent counts %v; want %v", counts, want)
	}
}
