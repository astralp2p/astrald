package nearby

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/ether"
	"github.com/cryptopunkscc/astrald/mod/ip"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// ReceiveObject handles ether broadcast and network-change events; it silently
// ignores drops that did not originate from the local node.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	// only receive objects from the local node
	if !drop.SenderID().IsEqual(mod.node.Identity()) {
		return nil
	}

	switch object := drop.Object().(type) {
	case *ether.EventBroadcastReceived:
		err := mod.receiveBroadcastEvent(object)
		if err == nil {
			return drop.Accept(false)
		}

	case *ip.EventNetworkAddressChanged:
		mod.receiveNetworkAddressChanged(object)
		return drop.Accept(false)
	}

	return nil
}

func (mod *Module) receiveBroadcastEvent(event *ether.EventBroadcastReceived) error {
	switch object := event.Object.(type) {
	case *nearby.StatusMessage:
		return mod.receiveStatus(event.SourceIP, object)

	case *nearby.ScanMessage:
		s := mod.Status(astral.Anyone)
		if mod.canBroadcast(s) {
			mod.Ether.PushToIP(event.SourceIP, s, nil)
		}
		return objects.ErrPushRejected
	}

	return objects.ErrPushRejected
}

func (mod *Module) receiveStatus(addr ip.IP, status *nearby.StatusMessage) error {
	mod.log.Logv(3, "update from %v", addr)

	entry := &cache{
		IP:        addr,
		Timestamp: time.Now(),
		Status:    status,
	}
	mod.cache.Replace(addr.String(), entry)

	go func() {
		if id := mod.ResolveStatus(status); id != nil {
			if e, ok := mod.cache.Get(addr.String()); ok {
				e.Identity = id
				mod.cache.Replace(addr.String(), e)
			}
		}
	}()

	return objects.ErrPushRejected
}

func (mod *Module) receiveNetworkAddressChanged(event *ip.EventNetworkAddressChanged) {
	if len(event.Added) == 0 {
		return // do nothing if there are no new network addresses
	}

	// broadcast our status
	mod.pushStatus()

	// scan for other nodes
	mod.Scan()
}
