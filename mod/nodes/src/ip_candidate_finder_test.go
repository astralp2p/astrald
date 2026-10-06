package nodes

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
)

func newObservedEndpointModule() (*Module, *recordingEvents) {
	events := &recordingEvents{}
	return &Module{Deps: Deps{Events: events}}, events
}

func TestAddObservedEndpointCapsCache(t *testing.T) {
	mod, events := newObservedEndpointModule()

	const added = ipCacheSize + 1
	for i := 1; i <= added; i++ {
		ep := tcpEndpoint(fmt.Sprintf("198.51.100.%d", i))
		mod.AddObservedEndpoint(ep, ep.IP)
	}

	if got := len(mod.observedEndpoints.Clone()); got != ipCacheSize {
		t.Fatalf("cache holds %d endpoints after %d additions; want %d", got, added, ipCacheSize)
	}

	emitted := events.recorded()
	if len(emitted) != added {
		t.Fatalf("emitted %d events; want %d", len(emitted), added)
	}
	for i, e := range emitted {
		if _, ok := e.(*nodes.NewObservedEndpointEvent); !ok {
			t.Fatalf("event %d is %T; want *nodes.NewObservedEndpointEvent", i, e)
		}
	}
}

func TestAddObservedEndpointDeduplicatesAddress(t *testing.T) {
	mod, _ := newObservedEndpointModule()

	for i := 0; i < 2; i++ {
		ep := tcpEndpoint("198.51.100.7")
		mod.AddObservedEndpoint(ep, ep.IP)
	}

	if got := len(mod.observedEndpoints.Clone()); got != 1 {
		t.Fatalf("cache holds %d entries after adding one address twice; want 1", got)
	}
}

func TestPublicIPCandidatesListsCachedIPs(t *testing.T) {
	mod, _ := newObservedEndpointModule()

	want := []string{"198.51.100.1", "198.51.100.2", "203.0.113.9"}
	for _, addr := range want {
		ep := tcpEndpoint(addr)
		mod.AddObservedEndpoint(ep, ep.IP)
	}

	var got []string
	for _, ip := range mod.PublicIPCandidates() {
		got = append(got, ip.String())
	}
	sort.Strings(got)

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("PublicIPCandidates() = %v; want %v", got, want)
	}
}

func TestCanAutoMigrate(t *testing.T) {
	cases := []struct {
		name  string
		age   time.Duration
		bytes uint64
		want  bool
	}{
		{"new session without traffic", 0, 0, false},
		{"new session with 1 MiB of traffic", 0, 1 << 20, true},
		{"session older than 30s", 31 * time.Second, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{createdAt: time.Now().Add(-tc.age)}
			s.bytes.Store(tc.bytes)

			if got := (&Module{}).canAutoMigrate(s); got != tc.want {
				t.Fatalf("canAutoMigrate(age %v, %d bytes) = %v; want %v", tc.age, tc.bytes, got, tc.want)
			}
		})
	}
}
