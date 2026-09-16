package frames

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// note: caller identity (33) + target identity (33) + nonce (8) + buffer (4) + query length (2).
const zeroRelayQueryLen = 80

// why: objects.new hands the zero value of a registered type to the encoder, and a panic stops the node.
func TestRelayQueryWriteTo_NilIdentities(t *testing.T) {
	obj := astral.New((&RelayQuery{}).ObjectType())
	if obj == nil {
		t.Fatal("nodes.frames.relay_query is not registered")
	}

	var buf bytes.Buffer
	n, err := obj.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if n != zeroRelayQueryLen {
		t.Errorf("wrote %d bytes, want %d", n, zeroRelayQueryLen)
	}
	if got := buf.Len(); got != zeroRelayQueryLen {
		t.Errorf("buffer holds %d bytes, want %d", got, zeroRelayQueryLen)
	}
}

// note: a nil identity encodes as the zero identity, so a zero RelayQuery decodes with zero identities.
func TestRelayQueryRoundTrip_NilIdentities(t *testing.T) {
	var buf bytes.Buffer
	if _, err := (&RelayQuery{}).WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got RelayQuery
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if got.CallerID == nil || !got.CallerID.IsZero() {
		t.Errorf("CallerID = %v, want the zero identity", got.CallerID)
	}
	if got.TargetID == nil || !got.TargetID.IsZero() {
		t.Errorf("TargetID = %v, want the zero identity", got.TargetID)
	}
}

// note: the guard substitutes the zero identity only for a nil field; a present identity encodes as itself.
func TestRelayQueryWriteTo_KeepsPresentIdentity(t *testing.T) {
	callerID := astral.GenerateIdentity()

	var buf bytes.Buffer
	if _, err := (&RelayQuery{CallerID: callerID}).WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got RelayQuery
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if !got.CallerID.IsEqual(callerID) {
		t.Errorf("CallerID = %v, want %v", got.CallerID, callerID)
	}
	if !got.TargetID.IsZero() {
		t.Errorf("TargetID = %v, want the zero identity", got.TargetID)
	}
}
