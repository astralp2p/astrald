package kcp

import "testing"

// Load keeps a valid configEndpoints entry and drops an entry ParseEndpoint rejects,
// so no nil *kcp.Endpoint reaches mod.configEndpoints.
func TestLoader_Load_DropsInvalidEndpoint(t *testing.T) {
	const config = `configEndpoints:
  - kcp:10.0.0.1:1792
  - kcp:10.0.0.2
`

	mod := loadModule(t, config)

	if len(mod.configEndpoints) != 1 {
		t.Fatalf("want 1 endpoint, got %d: %v", len(mod.configEndpoints), mod.configEndpoints)
	}
	if mod.configEndpoints[0] == nil {
		t.Fatal("nil endpoint stored")
	}
	if got := mod.configEndpoints[0].Address(); got != "10.0.0.1:1792" {
		t.Fatalf("want 10.0.0.1:1792, got %v", got)
	}
}
