package tcp

import (
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral/log"
)

// noLocalIPs is an ip module with no local interfaces, so endpoints() yields
// only the configured endpoints.
type noLocalIPs struct{}

func (noLocalIPs) LocalIPs() ([]ip.IP, error)     { return nil, nil }
func (noLocalIPs) PublicIPCandidates() []ip.IP    { return nil }
func (noLocalIPs) DefaultGateway() (ip.IP, error) { return nil, nil }

// Load keeps a valid configEndpoints entry and drops an entry ParseEndpoint
// rejects, so no nil *tcp.Endpoint reaches PublicIPCandidates.
func TestLoader_Load_DropsInvalidEndpoint(t *testing.T) {
	const config = `configEndpoints:
  - tcp:8.8.8.8:1791
  - tcp:192.168.1.10
`

	// note: the logger is real because Load logs the invalid entry, and a nil
	// *log.Logger has no nil guard.
	logger := log.New(nil)
	logger.SetFilter(func(*log.Entry) bool { return false })

	loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, logger)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mod := loaded.(*Module)
	mod.IP = noLocalIPs{}

	if len(mod.configEndpoints) != 1 {
		t.Fatalf("want 1 endpoint, got %d: %v", len(mod.configEndpoints), mod.configEndpoints)
	}
	if got := mod.configEndpoints[0].Address(); got != "8.8.8.8:1791" {
		t.Fatalf("want 8.8.8.8:1791, got %v", got)
	}

	candidates := mod.PublicIPCandidates()
	if len(candidates) != 1 || candidates[0].String() != "8.8.8.8" {
		t.Fatalf("want [8.8.8.8], got %v", candidates)
	}
}
