package messaging

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// routedQuery is one query a recordingNode routed: its path, the caller it
// names, and the identity the routing context carries.
type routedQuery struct {
	path    string
	caller  *astral.Identity
	context *astral.Identity
}

// recordingNode remembers every query it routes, then routes it on.
type recordingNode struct {
	loopbackNode
	mu     sync.Mutex
	routed []routedQuery
}

func (n *recordingNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.mu.Lock()
	n.routed = append(n.routed, routedQuery{
		path:    string(q.QueryString),
		caller:  q.Caller,
		context: ctx.Identity(),
	})
	n.mu.Unlock()

	return n.loopbackNode.RouteQuery(ctx, q, w)
}

// find answers the first routed query on path, if any.
func (n *recordingNode) find(path string) (routedQuery, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for _, r := range n.routed {
		if r.path == path {
			return r, true
		}
	}
	return routedQuery{}, false
}

// A delivery and a receipt are routed on the node's own context and name the
// participant only as the caller. A link relays a query whose caller is not
// the context's identity, naming the caller and the target; a query whose
// caller is the context's identity crosses as the node's own, and the far node
// reads it as this node querying itself.
func TestDeliveriesAndReceiptsAreRoutedAsTheNode(t *testing.T) {
	mod := testMessagingModule(t)
	nodeID := mod.node.Identity()
	mod.ctx = mod.ctx.WithIdentity(nodeID)
	node := &recordingNode{loopbackNode: loopbackNode{identity: nodeID, router: mod}}
	mod.node = node

	sender, recipient := hostedParticipant(t, mod), hostedParticipant(t, mod)
	_, err := mod.SendMessage(context.Background(), sender, &messaging.SendMessageRequest{
		To:      astral.String8(recipient.String()),
		Content: "near",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// a message from a sender this node does not host owes a receipt when read
	far := &messaging.Message{ID: messaging.NewMessageID(), Content: "far"}
	deliverOverRouter(t, mod, recipient, far)
	_, err = mod.ReadMessages(context.Background(), recipient, &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: far.ID}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	delivery := waitRouted(t, node, messaging.MethodMessage)
	receipt := waitRouted(t, node, messaging.MethodReceipt)

	for _, c := range []struct {
		got    routedQuery
		caller *astral.Identity
	}{{delivery, sender}, {receipt, recipient}} {
		if !c.got.context.IsEqual(nodeID) {
			t.Errorf("%v routed on a context naming %v, not the node %v", c.got.path, c.got.context, nodeID)
		}
		if !c.got.caller.IsEqual(c.caller) {
			t.Errorf("%v names caller %v, want %v", c.got.path, c.got.caller, c.caller)
		}
	}
}

// waitRouted answers the first query routed on path, waiting for one sent
// from a goroutine the caller does not hold.
func waitRouted(t *testing.T, node *recordingNode, path string) routedQuery {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if r, ok := node.find(path); ok {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing was routed on %v", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
