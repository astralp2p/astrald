package gateway

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/gateway"
	gatewayClient "github.com/cryptopunkscc/astrald/mod/gateway/client"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
)

var _ scheduler.Task = &MaintainGatewayConnectionsTask{}
var _ scheduler.EventReceiver = &MaintainGatewayConnectionsTask{}

// MaintainGatewayConnectionsTask keeps the node perpetually registered with a
// single gateway, retrying with exponential back-off on failure and waking
// immediately when a new public IP address is observed.
type MaintainGatewayConnectionsTask struct {
	mod        *Module
	GatewayID  *astral.Identity
	Visibility gateway.Visibility
	retry      *sig.Retry
	wakeCh     chan struct{}
}

func (mod *Module) NewMaintainGatewayConnectionsTask(gatewayID *astral.Identity, visibility gateway.Visibility) *MaintainGatewayConnectionsTask {
	retry, _ := sig.NewRetry(time.Second, 15*time.Minute, 2)
	return &MaintainGatewayConnectionsTask{
		mod:        mod,
		GatewayID:  gatewayID,
		Visibility: visibility,
		retry:      retry,
		wakeCh:     make(chan struct{}, 1),
	}
}

func (task *MaintainGatewayConnectionsTask) String() string {
	return "maintain_gateway_connections_task"
}

func (task *MaintainGatewayConnectionsTask) Run(ctx *astral.Context) error {
	task.mod.log.Log("starting to maintain connections to %v", task.GatewayID)
	client := gatewayClient.New(task.GatewayID, astrald.Default())

	count := -1
	for {
		switch {
		case count == 0:
			task.mod.log.Log("binding to %v lost, rebinding", task.GatewayID)
		case count > 0 && count%5 == 0:
			task.mod.log.Log("still trying to register to %v (attempt %v)", task.GatewayID, count)
		}

		socket, err := client.Register(ctx.IncludeZone(astral.ZoneNetwork), task.Visibility)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case count = <-task.retry.Retry():
			case <-task.wakeCh:
			}
			continue
		}

		task.retry.Reset()
		if count > 0 {
			task.mod.log.Log("rebound to %v after %v attempts", task.GatewayID, count)
		} else if count < 0 {
			task.mod.log.Log("bound to %v", task.GatewayID)
		}
		count = 0

		err = task.mod.newConnPool(ctx, task.GatewayID, *socket).Run()
		if err != nil {
			task.mod.log.Error("rebinding to %v due to: %v", task.GatewayID, err)
		}
	}
}

// ReceiveEvent wakes the registration loop when a new public IP address appears,
// allowing faster reconnection after a network change without waiting for the
// current back-off timer to expire.
func (task *MaintainGatewayConnectionsTask) ReceiveEvent(e *events.Event) {
	switch typed := e.Data.(type) {
	case *ip.EventNetworkAddressChanged:
		if len(typed.Added) > 0 {
			task.retry.Reset()
			select {
			case task.wakeCh <- struct{}{}:
			default:
			}
		}
	}
}
