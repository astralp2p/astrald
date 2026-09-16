package objects

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// identityNode is an astral.Node that only answers Identity; every other method
// panics on the embedded nil interface.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// senderRecorder is a receiver that keeps the sender of every drop and accepts none.
type senderRecorder struct {
	mu      sync.Mutex
	senders []*astral.Identity
}

func (r *senderRecorder) ReceiveObject(drop objectsmod.Drop) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senders = append(r.senders, drop.SenderID())
	return nil
}

func (r *senderRecorder) recorded() []*astral.Identity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*astral.Identity(nil), r.senders...)
}

// TestReceiveDispatchesAsTheSource pins the sender a receiver sees: the source
// Receive was given, with a zero source standing for the local node. Receivers
// guard local events on the sender, so a remote source must stay remote — ether
// delivers a LAN broadcast's object under its signed source (mod/ether/src/module.go).
func TestReceiveDispatchesAsTheSource(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	remoteID := astral.GenerateIdentity()

	cases := []struct {
		name   string
		source *astral.Identity
		want   *astral.Identity
	}{
		{"a remote source stays remote", remoteID, remoteID},
		{"a local source stays local", nodeID, nodeID},
		{"a zero source is the local node", nil, nodeID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &senderRecorder{}
			mod := &Module{node: &identityNode{id: nodeID}}
			if err := mod.AddReceiver(recorder); err != nil {
				t.Fatalf("add receiver: %v", err)
			}

			event := &events.Event{ID: astral.NewNonce(), SourceID: nodeID, Data: &astral.Nil{}}

			// the recorder accepts nothing, so Receive reports a rejection
			if err := mod.Receive(event, tc.source); err == nil {
				t.Fatal("Receive reported acceptance; no receiver accepted the object")
			}

			senders := recorder.recorded()
			if len(senders) != 1 {
				t.Fatalf("receiver was offered %d drops; want 1", len(senders))
			}

			if !senders[0].IsEqual(tc.want) {
				t.Fatalf("receiver saw sender %v; want %v", senders[0], tc.want)
			}
		})
	}
}
