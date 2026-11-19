package gateway

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	gatewayClient "github.com/astralp2p/astral-go/api/gateway/client"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/scheduler"
)

var _ scheduler.Task = &MaintainGatewayConnectionsTask{}
var _ scheduler.EventReceiver = &MaintainGatewayConnectionsTask{}

// MaintainGatewayConnectionsTask keeps the node perpetually registered with a
// single gateway, retrying with exponential back-off on failure and waking
// immediately when a new public IP address is observed.
type MaintainGatewayConnectionsTask struct {
	mod        *Module
	GatewayID  *astral.Identity
	Endpoints  []exonet.Endpoint // the gateway's configured link endpoints
	Visibility gateway.Visibility
	retry      *sig.Retry
	wakeCh     chan struct{}

	register        func(*astral.Context) (*gateway.Socket, error) // registers with the gateway
	holdPool        func(*astral.Context, gateway.Socket) error    // holds the idle conn pool until it fails
	refreshInterval time.Duration                                  // how often the gateway's rows are stored again while registered
}

func (mod *Module) NewMaintainGatewayConnectionsTask(gatewayID *astral.Identity, endpoints []exonet.Endpoint, visibility gateway.Visibility) *MaintainGatewayConnectionsTask {
	retry, _ := sig.NewRetry(time.Second, 15*time.Minute, 2)
	return &MaintainGatewayConnectionsTask{
		mod:             mod,
		GatewayID:       gatewayID,
		Endpoints:       endpoints,
		Visibility:      visibility,
		retry:           retry,
		wakeCh:          make(chan struct{}, 1),
		refreshInterval: endpointRefreshInterval,
		register: func(ctx *astral.Context) (*gateway.Socket, error) {
			client := gatewayClient.New(gatewayID, astrald.Default())
			return client.Register(ctx.IncludeZone(astral.ZoneNetwork), visibility)
		},
		holdPool: func(ctx *astral.Context, socket gateway.Socket) error {
			return mod.newConnPool(ctx, gatewayID, socket).Run()
		},
	}
}

func (task *MaintainGatewayConnectionsTask) String() string {
	return "maintain_gateway_connections_task"
}

func (task *MaintainGatewayConnectionsTask) Run(ctx *astral.Context) error {
	task.mod.log.Log("starting to maintain connections to %v", task.GatewayID)

	count := -1
	for {
		switch {
		case count == 0:
			task.mod.log.Log("binding to %v lost, rebinding", task.GatewayID)
		case count > 0 && count%5 == 0:
			task.mod.log.Log("still trying to register to %v (attempt %v)", task.GatewayID, count)
		}

		socket, err := task.register(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case count = <-task.retry.Retry():
			case <-task.wakeCh:
			}
			// why: re-registration must find the gateway's rows unexpired, however long the node has run.
			task.mod.storeGatewayEndpoints(task.GatewayID, task.Endpoints)
			continue
		}

		task.retry.Reset()
		if count > 0 {
			task.mod.log.Log("rebound to %v after %v attempts", task.GatewayID, count)
		} else if count < 0 {
			task.mod.log.Log("bound to %v", task.GatewayID)
		}
		count = 0

		err = task.holdRegistration(ctx, *socket)
		if err != nil {
			task.mod.log.Error("rebinding to %v due to: %v", task.GatewayID, err)
		}
		task.mod.storeGatewayEndpoints(task.GatewayID, task.Endpoints)
	}
}

// holdRegistration marks the gateway registered and holds the idle conn pool until it ends.
// The gateway's rows are stored again at the start and every refreshInterval until then.
func (task *MaintainGatewayConnectionsTask) holdRegistration(ctx *astral.Context, socket gateway.Socket) error {
	task.mod.storeGatewayEndpoints(task.GatewayID, task.Endpoints)
	task.mod.registered.Set(task.GatewayID.String(), struct{}{})
	defer task.mod.registered.Delete(task.GatewayID.String())

	refreshCtx, cancel := ctx.WithCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		task.refreshEndpoints(refreshCtx)
	}()

	err := task.holdPool(ctx, socket)
	cancel()
	wg.Wait()
	return err
}

// refreshEndpoints stores the gateway's rows every refreshInterval until ctx ends.
// why: a registration can outlive the rows' TTL, and every query to the gateway routes through them.
func (task *MaintainGatewayConnectionsTask) refreshEndpoints(ctx *astral.Context) {
	ticker := time.NewTicker(task.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task.mod.storeGatewayEndpoints(task.GatewayID, task.Endpoints)
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
