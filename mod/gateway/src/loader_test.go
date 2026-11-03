package gateway

import (
	"testing"

	"github.com/astralp2p/astral-go/api/gateway"
)

// TestLoadDefaultsToPrivateVisibility pins that a node without gateway.yaml
// registers with its gateways as private. Visibility is never stored, so the
// same default holds on a fresh and on an upgraded node.
func TestLoadDefaultsToPrivateVisibility(t *testing.T) {
	loaded, err := Loader{}.Load(nil, noAssets{}, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := loaded.(*Module).config.Visibility; got != gateway.VisibilityPrivate {
		t.Errorf("visibility: got %v, want %v", got, gateway.VisibilityPrivate)
	}
}
