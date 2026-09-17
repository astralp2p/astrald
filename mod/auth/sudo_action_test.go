package auth

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func TestSudoActionRoundTrip(t *testing.T) {
	actor, as := astral.GenerateIdentity(), astral.GenerateIdentity()
	want := &SudoAction{Action: auth.NewAction(actor), AsID: as}

	cases := []struct {
		name      string
		roundTrip func(*testing.T) *SudoAction
	}{
		{"astral.Encode and DecodeAs", func(t *testing.T) *SudoAction {
			var buf bytes.Buffer
			if _, err := astral.Encode(&buf, want); err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := astral.DecodeAs[*SudoAction](buf.Bytes())
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			return got
		}},
		{"WriteTo and ReadFrom", func(t *testing.T) *SudoAction {
			var buf bytes.Buffer
			if _, err := want.WriteTo(&buf); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := &SudoAction{}
			if _, err := got.ReadFrom(&buf); err != nil {
				t.Fatalf("read: %v", err)
			}
			return got
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.roundTrip(t)

			if got.Nonce != want.Nonce {
				t.Errorf("Nonce = %v, want %v", got.Nonce, want.Nonce)
			}
			if !got.ActorID.IsEqual(actor) {
				t.Errorf("ActorID = %v, want %v", got.ActorID, actor)
			}
			if !got.AsID.IsEqual(as) {
				t.Errorf("AsID = %v, want %v", got.AsID, as)
			}
		})
	}
}
