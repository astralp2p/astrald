package nearby

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nearby"
)

func cacheStatus(mod *Module, sourceIP string, stamped time.Time, status *nearby.StatusMessage) {
	mod.cache.Replace(sourceIP, &cache{
		IP:        ip.IP(net.ParseIP(sourceIP)),
		Timestamp: stamped,
		Status:    status,
	})
}

func alias(s string) *dir.Alias {
	a := dir.Alias(s)
	return &a
}

func tcpEndpoint(host string, port astral.Uint16) *nodes.EndpointWithTTL {
	return nodes.NewEndpointWithTTL(&tcp.Endpoint{IP: ip.IP(net.ParseIP(host)), Port: port}, time.Hour)
}

func kcpEndpoint(host string, port astral.Uint16) *nodes.EndpointWithTTL {
	return nodes.NewEndpointWithTTL(&kcp.Endpoint{IP: ip.IP(net.ParseIP(host)), Port: port}, time.Hour)
}

func countEndpoints(t *testing.T, mod *Module, nodeID *astral.Identity) int {
	t.Helper()

	ch, err := mod.ResolveEndpoints(astral.NewContext(nil), nodeID)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}

	timeout := time.After(2 * time.Second)
	count := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return count
			}
			count++
		case <-timeout:
			t.Fatalf("ResolveEndpoints channel not closed after %d endpoints", count)
		}
	}
}

func TestResolveIdentityByAlias(t *testing.T) {
	n := astral.GenerateIdentity()
	mod := &Module{}
	cacheStatus(mod, "192.168.1.20", time.Now(), statusWith(t, &nearby.PublicProfile{NodeID: n}, alias("phone")))

	got, err := mod.ResolveIdentity(".phone")
	if err != nil {
		t.Fatalf("ResolveIdentity(.phone): %v", err)
	}
	if !got.IsEqual(n) {
		t.Errorf("ResolveIdentity(.phone) = %v; want %v", got, n)
	}

	for _, s := range []string{"phone", ".tablet"} {
		if got, err := mod.ResolveIdentity(s); err == nil {
			t.Errorf("ResolveIdentity(%q) = %v; want an error", s, got)
		}
	}
}

func TestResolveIdentitySkipsAnEntryWithoutASender(t *testing.T) {
	mod := &Module{}
	cacheStatus(mod, "192.168.1.20", time.Now(), statusWith(t, alias("phone")))

	if got, err := mod.ResolveIdentity(".phone"); err == nil {
		t.Errorf("ResolveIdentity(.phone) = %v; want an error for an entry that names no sender", got)
	}
}

func TestResolveIdentityExpiresStaleEntries(t *testing.T) {
	n := astral.GenerateIdentity()
	mod := &Module{}
	cacheStatus(mod, "192.168.1.20", time.Now().Add(-6*time.Minute), statusWith(t, &nearby.PublicProfile{NodeID: n}, alias("phone")))

	if got, err := mod.ResolveIdentity(".phone"); err == nil {
		t.Errorf("ResolveIdentity(.phone) on stale entry = %v; want an error", got)
	}
	if l := mod.cache.Len(); l != 0 {
		t.Errorf("cache.Len() = %d; want 0 after expiry", l)
	}
}

func TestResolveEndpointsFiltersByIdentity(t *testing.T) {
	n := astral.GenerateIdentity()
	mod := &Module{}
	cacheStatus(mod, "192.168.1.20", time.Now(), statusWith(t,
		&nearby.PublicProfile{NodeID: n},
		tcpEndpoint("192.168.1.20", 1791),
		kcpEndpoint("192.168.1.20", 1792),
	))

	if got := countEndpoints(t, mod, n); got != 2 {
		t.Errorf("ResolveEndpoints(node) yielded %d; want 2", got)
	}
	if got := countEndpoints(t, mod, astral.GenerateIdentity()); got != 0 {
		t.Errorf("ResolveEndpoints(other) yielded %d; want 0", got)
	}
}

func TestResolveEndpointsMergesEntries(t *testing.T) {
	n := astral.GenerateIdentity()
	mod := &Module{}
	cacheStatus(mod, "192.168.1.20", time.Now(), statusWith(t, &nearby.PublicProfile{NodeID: n}, tcpEndpoint("192.168.1.20", 1791)))
	cacheStatus(mod, "10.0.0.20", time.Now(), statusWith(t, &nearby.PublicProfile{NodeID: n}, tcpEndpoint("10.0.0.20", 1791)))

	if got := countEndpoints(t, mod, n); got != 2 {
		t.Errorf("ResolveEndpoints(node) yielded %d; want 2", got)
	}
}
