package frames

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestFrameBlueprintsRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		obj  astral.Object
	}{
		{"ping", &Ping{Nonce: astral.NewNonce(), Pong: false}},
		{"pong", &Ping{Nonce: astral.NewNonce(), Pong: true}},
		{"query", &Query{Nonce: astral.NewNonce(), Buffer: 12345, Query: "hello"}},
		{"relay_query", &RelayQuery{
			CallerID: astral.GenerateIdentity(),
			TargetID: astral.GenerateIdentity(),
			Query:    Query{Nonce: astral.NewNonce(), Buffer: 12345, Query: "hello"},
		}},
		{"response", &Response{Nonce: astral.NewNonce(), ErrCode: 1, Buffer: 10}},
		{"read", &Read{Nonce: astral.NewNonce(), Len: 4096}},
		{"data", &Data{Nonce: astral.NewNonce(), Payload: []byte("payload")}},
		{"migrate", &Migrate{Nonce: astral.NewNonce()}},
		{"reset", &Reset{Nonce: astral.NewNonce()}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var data = &bytes.Buffer{}
			_, err := astral.Encode(data, tc.obj, astral.WithEncoder(FrameTypeEncoder))
			if err != nil {
				t.Fatalf("pack failed: %v", err)
			}

			obj, _, err := astral.Decode(data, astral.WithDecoder(FrameTypeDecoder))
			if err != nil {
				t.Fatalf("unpack failed: %v", err)
			}

			if reflect.TypeOf(obj) != reflect.TypeOf(tc.obj) {
				t.Fatalf("type mismatch: got %T want %T", obj, tc.obj)
			}

			if !reflect.DeepEqual(obj, tc.obj) {
				t.Fatalf("value mismatch: got %#v want %#v", obj, tc.obj)
			}
		})
	}
}
