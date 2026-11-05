package nearby

import (
	"errors"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astrald/mod/objects"
)

// ReceiveObject handles the ether broadcasts and network address changes the local node
// delivers; it ignores drops from any other sender and never accepts one.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	if !drop.SenderID().IsEqual(mod.node.Identity()) {
		return nil
	}

	switch object := drop.Object().(type) {
	case *ether.EventBroadcastReceived:
		mod.receiveBroadcast(object)

	case *events.Event:
		if changed, ok := object.Data.(*ip.EventNetworkAddressChanged); ok {
			mod.receiveNetworkAddressChanged(changed)
		}
	}

	return nil
}

// receiveBroadcast handles one object a nearby node broadcast.
func (mod *Module) receiveBroadcast(event *ether.EventBroadcastReceived) {
	switch object := event.Object.(type) {
	case *nearby.StatusMessage:
		mod.receiveStatus(event.SourceIP, object)

	case *nearby.ScanMessage:
		mod.replyToScan(event.SourceIP)
	}
}

// receiveStatus caches a status that passes every receive check and logs why one is dropped.
func (mod *Module) receiveStatus(src ip.IP, status *nearby.StatusMessage) {
	err := mod.acceptStatus(src, status)
	switch {
	case err == nil:
	case errors.Is(err, errPanic):
		mod.log.Error("dropped status from %v: %v", src, err)
	default:
		mod.log.Logv(2, "dropped status from %v: %v", src, err)
	}
}

// receiveNetworkAddressChanged sends the status and then a scan when the node gains an address.
func (mod *Module) receiveNetworkAddressChanged(event *ip.EventNetworkAddressChanged) {
	if len(event.Added) == 0 {
		return
	}

	if err := mod.Broadcast(); err != nil {
		mod.log.Error("broadcast status: %v", err)
	}
	if err := mod.Scan(); err != nil {
		mod.log.Error("scan: %v", err)
	}
}
