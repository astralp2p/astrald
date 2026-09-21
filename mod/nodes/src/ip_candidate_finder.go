package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
)

const ipCacheSize = 8

type ObservedEndpoint struct {
	Endpoint exonet.Endpoint
	IP       ip.IP
	Observed int64
}

func (mod *Module) PublicIPCandidates() (list []ip.IP) {
	cache := mod.observedEndpoints.Clone()
	list = make([]ip.IP, 0, len(cache))
	for _, entry := range cache {
		list = append(list, entry.IP)
	}
	return list
}

// AddObservedEndpoint adds or updates an IP in the cache, evicting the oldest other
// entry if over size; the entry just added is never evicted.
func (mod *Module) AddObservedEndpoint(endpoint exonet.Endpoint, ip ip.IP) {
	key := endpoint.Address()
	mod.observedEndpoints.Set(key, ObservedEndpoint{
		Endpoint: endpoint,
		IP:       ip,
		Observed: time.Now().UnixNano(),
	})

	mod.Events.Emit(&nodes.NewObservedEndpointEvent{})

	cache := mod.observedEndpoints.Clone()
	if len(cache) > ipCacheSize {
		// Find the oldest entry
		var oldestKey string
		var oldestTime int64
		first := true
		for k, v := range cache {
			// why: the clone already holds the entry just added, and a tie or a
			// backward clock step would otherwise make it the eviction victim.
			if k == key {
				continue
			}
			if first || v.Observed < oldestTime {
				oldestTime = v.Observed
				oldestKey = k
				first = false
			}
		}

		if oldestKey != "" {
			mod.observedEndpoints.Delete(oldestKey)
		}
	}
}
