package crypto

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// encodeUnknownType writes a well-framed binary object whose type is not
// registered: the "unknown type tag on a binary channel" receive failure.
// Framing stays valid, so the channel fails on the decode and not on a
// truncated read.
func encodeUnknownType(t *testing.T, w io.Writer, typeName string) {
	t.Helper()

	if _, err := astral.ObjectType(typeName).WriteTo(w); err != nil {
		t.Fatalf("encode type: %v", err)
	}
	if _, err := astral.Bytes32(nil).WriteTo(w); err != nil {
		t.Fatalf("encode payload: %v", err)
	}
}

// TestOpsReportDecodeErrorBeforeClosing: each hand-rolled crypto op answers an
// input the channel cannot decode with an error_message before it closes, and
// the message names the type that failed.
//
// why: the ops returned the Switch error with nothing sent, so the channel
// closed bare and the peer could not tell a rejected payload from a dropped
// transport. dc89aa71 removed the report when it rewrote these five ops by
// hand; nothing here failed until 0cdca707 restored it. channel.Batch, which
// the other eleven ops use, pins the same invariant in astral-go
// (astral/channel/batch_test.go, TestBatch_ReceiveError_ReportsBeforeClosing).
func TestOpsReportDecodeErrorBeforeClosing(t *testing.T) {
	mod := &Module{}

	for _, op := range []struct {
		name string
		fn   any
	}{
		{"crypto.public_key", mod.OpPublicKey},
		{"crypto.sign_text", mod.OpSignText},
		{"crypto.sign_hash", mod.OpSignHash},
		{"crypto.verify_text_signature", mod.OpVerifyTextSignature},
		{"crypto.verify_hash_signature", mod.OpVerifyHashSignature},
	} {
		t.Run(op.name, func(t *testing.T) {
			rig := startOp(t, op.name, op.fn)

			unknownType := "unregistered.x." + strings.TrimPrefix(op.name, "crypto.")
			encodeUnknownType(t, rig.raw, unknownType)

			switch object := rig.receiveOne(t).(type) {
			case *astral.ErrorMessage:
				if !strings.Contains(object.Error(), unknownType) {
					t.Fatalf("error_message is %q; want it to name %q", object.Error(), unknownType)
				}
			default:
				t.Fatalf("answer is %v; want error_message", object.ObjectType())
			}

			// the op still returns the failure: the report is added to the
			// error path, it does not swallow it
			err := rig.opError()
			if err == nil {
				t.Fatal("op returned nil; want the receive error")
			}
			if !errors.Is(err, astral.ErrBlueprintNotFound) {
				t.Fatalf("op returned %v; want ErrBlueprintNotFound", err)
			}
		})
	}
}
