package nearby

import (
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
)

// note: these methods exist only in this package's tests. They let the in-process node tests
// (lan_node_test.go) carry a status from one node to another without ether, which drops the
// datagrams of its own host.

// ComposeStatus returns the status the node would broadcast now.
func (mod *Module) ComposeStatus() (*nearby.StatusMessage, error) {
	return mod.status()
}

// DeliverStatus runs the receive checks on a status from src, as a broadcast would.
func (mod *Module) DeliverStatus(src ip.IP, status *nearby.StatusMessage) error {
	return mod.acceptStatus(src, status)
}
