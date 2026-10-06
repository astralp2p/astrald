package tor

import (
	"testing"

	"github.com/astralp2p/astrald/mod/tor/tc"
)

// TestListenerEndpointUsesListenPort checks the advertised endpoint port.
//
// why: the onion is registered on config.ListenPort, so the endpoint must not
// fall back to defaultListenPort.
func TestListenerEndpointUsesListenPort(t *testing.T) {
	l := listener{onion: tc.Onion{ServiceID: testServiceID}}

	e, err := l.Endpoint(4000)
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if e.Port != 4000 {
		t.Fatalf("endpoint port = %d, want 4000", e.Port)
	}
}
