package nodes

import (
	"fmt"
	"testing"
)

// TestAddObservedEndpointKeepsNewestEndpoint pins the eviction rule: filling the
// cache past ipCacheSize drops an older entry, never the entry just added.
//
// why the rounds: the defect this guards picked its victim by map iteration
// order among entries sharing a one-second timestamp, so a single fill misses it
// most of the time. Against the fix the test is deterministic — the inserted key
// is unselectable whatever the map order or the clock does.
func TestAddObservedEndpointKeepsNewestEndpoint(t *testing.T) {
	const rounds = 200

	for round := 0; round < rounds; round++ {
		mod := &Module{Deps: Deps{Events: &recordingEvents{}}}

		var newestAddress string
		for i := 1; i <= ipCacheSize+1; i++ {
			endpoint := tcpEndpoint(fmt.Sprintf("198.51.100.%d", i))
			newestAddress = endpoint.Address()
			mod.AddObservedEndpoint(endpoint, endpoint.IP)
		}

		cache := mod.observedEndpoints.Clone()
		if len(cache) != ipCacheSize {
			t.Fatalf("round %d: cache holds %d endpoints; want %d", round, len(cache), ipCacheSize)
		}
		if _, ok := cache[newestAddress]; !ok {
			t.Fatalf("round %d: the endpoint just added (%v) was evicted", round, newestAddress)
		}
	}
}
