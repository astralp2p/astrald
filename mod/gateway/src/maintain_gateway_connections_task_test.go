package gateway

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	eventssrc "github.com/astralp2p/astrald/mod/events/src"
	ipsrc "github.com/astralp2p/astrald/mod/ip/src"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	schedulersrc "github.com/astralp2p/astrald/mod/scheduler/src"
)

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// noAssets is an assets.Assets with no stored configuration.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it. Any method other than LoadYAML panics.
type noAssets struct {
	assets.Assets
}

func (noAssets) LoadYAML(string, interface{}) error { return nil }

// localDrop is an objects.Drop that no receiver needs to accept.
type localDrop struct {
	sender *astral.Identity
	object astral.Object
}

func (d *localDrop) SenderID() *astral.Identity { return d.sender }
func (d *localDrop) Object() astral.Object      { return d.object }
func (d *localDrop) Accept(bool) error          { return nil }

// dispatchObjects hands every received object to one receiver, as the objects
// module hands it to every registered receiver.
//
// why: the embedded nil interface satisfies objectsmod.Module without
// implementing it. Any method other than Receive panics.
type dispatchObjects struct {
	objectsmod.Module
	node     astral.Node
	receiver objectsmod.Receiver
}

func (o *dispatchObjects) Receive(obj astral.Object, source *astral.Identity) error {
	if source.IsZero() {
		source = o.node.Identity()
	}
	return o.receiver.ReceiveObject(&localDrop{sender: source, object: obj})
}

// idleGatewayTask is a MaintainGatewayConnectionsTask that never registers.
// ReceiveEvent and String are the embedded task's own.
type idleGatewayTask struct {
	*MaintainGatewayConnectionsTask
	started chan struct{}
}

func (t idleGatewayTask) Run(ctx *astral.Context) error {
	close(t.started)
	<-ctx.Done()
	return nil
}

// stubAddressChange makes the second and later interface reads report one more address.
func stubAddressChange(t *testing.T) {
	t.Helper()

	var reads atomic.Int32
	orig := ipsrc.InterfaceAddrs
	t.Cleanup(func() { ipsrc.InterfaceAddrs = orig })
	ipsrc.InterfaceAddrs = func() ([]net.Addr, error) {
		addrs := []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.2"), Mask: net.CIDRMask(24, 32)}}
		if reads.Add(1) > 1 {
			addrs = append(addrs, &net.IPNet{IP: net.ParseIP("192.168.1.3"), Mask: net.CIDRMask(24, 32)})
		}
		return addrs, nil
	}
}

// loadEventPath loads the scheduler, events and ip modules, with the events
// module dispatching its objects to the scheduler, and runs the scheduler.
func loadEventPath(t *testing.T, ctx *astral.Context, node astral.Node) (*schedulersrc.Module, *ipsrc.Module) {
	t.Helper()
	l := log.New(node.Identity())

	schedMod, err := schedulersrc.Loader{}.Load(node, noAssets{}, l)
	if err != nil {
		t.Fatalf("load scheduler: %v", err)
	}
	sched := schedMod.(*schedulersrc.Module)

	eventsMod, err := eventssrc.Loader{}.Load(node, noAssets{}, l)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	events := eventsMod.(*eventssrc.Module)
	events.Objects = &dispatchObjects{node: node, receiver: sched}

	ipMod, err := ipsrc.Loader{}.Load(node, noAssets{}, l)
	if err != nil {
		t.Fatalf("load ip: %v", err)
	}
	ip := ipMod.(*ipsrc.Module)
	ip.Events = events

	go sched.Run(ctx)
	<-sched.Ready()
	return sched, ip
}

// A new address on a network interface reaches the running gateway task's
// ReceiveEvent through the ip, events and scheduler modules, and wakes its
// registration loop.
func TestNetworkAddressChangeWakesGatewayTask(t *testing.T) {
	stubAddressChange(t)

	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	t.Cleanup(cancel)

	node := &identityNode{id: astral.GenerateIdentity()}
	sched, ip := loadEventPath(t, ctx, node)

	task := (&Module{}).NewMaintainGatewayConnectionsTask(astral.GenerateIdentity(), gateway.VisibilityPublic)
	idle := idleGatewayTask{MaintainGatewayConnectionsTask: task, started: make(chan struct{})}
	if _, err := sched.Schedule(idle); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	<-idle.started

	go ip.Run(ctx)

	select {
	case <-task.wakeCh:
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway task was not woken within 10s of the address change")
	}
}
