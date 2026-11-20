package frames

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/astral/channel"
)

// TestFrameTypesAppendOnly pins the opcode order: every earlier entry keeps
// its index, and nodes.frames.close is the last entry.
func TestFrameTypesAppendOnly(t *testing.T) {
	want := []string{
		"nodes.frames.ping",
		"nodes.frames.query",
		"nodes.frames.relay_query",
		"nodes.frames.read",
		"nodes.frames.response",
		"nodes.frames.data",
		"nodes.frames.migrate",
		"nodes.frames.reset",
		"nodes.frames.close",
	}
	if !reflect.DeepEqual(FrameTypes, want) {
		t.Fatalf("FrameTypes = %v; want %v", FrameTypes, want)
	}
}

// TestCloseWireLayout pins the payload to the spec: one uint8 Reason
// (astral-docs topics/link-mux.md, close.Reason 0 closed, 1 excess, 2 shutdown).
func TestCloseWireLayout(t *testing.T) {
	for reason, want := range map[CloseReason]byte{CloseClosed: 0, CloseExcess: 1, CloseShutdown: 2} {
		var buf bytes.Buffer
		if _, err := (&Close{Reason: reason}).WriteTo(&buf); err != nil {
			t.Fatalf("WriteTo(%v): %v", reason, err)
		}
		if got := buf.Bytes(); !bytes.Equal(got, []byte{want}) {
			t.Fatalf("close(%v) payload = %x; want %x", reason, got, want)
		}
	}
}

// TestCloseDecodesByTypeName sends a Close over a link-style channel, which
// names each frame by its type name, and receives it as a *Close.
// why: the live link decodes a frame through its astral.Add registration, not FrameTypes.
func TestCloseDecodesByTypeName(t *testing.T) {
	var wire bytes.Buffer
	sent := &Close{Reason: CloseExcess}
	if err := channel.New(&wire).Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}
	obj, err := channel.New(&wire).Receive()
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	got, ok := obj.(*Close)
	if !ok {
		t.Fatalf("received %T; want *frames.Close", obj)
	}
	if *got != *sent {
		t.Fatalf("received %v; want %v", got, sent)
	}
}

// TestCloseReasonKnown: a reason this node does not know reads as closed.
func TestCloseReasonKnown(t *testing.T) {
	for in, want := range map[CloseReason]CloseReason{
		CloseClosed: CloseClosed, CloseExcess: CloseExcess, CloseShutdown: CloseShutdown, 3: CloseClosed, 255: CloseClosed,
	} {
		if got := in.Known(); got != want {
			t.Fatalf("CloseReason(%d).Known() = %v; want %v", uint8(in), got, want)
		}
	}
}
