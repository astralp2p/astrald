package frames

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

func TestQueryMaxLengthRoundTrip(t *testing.T) {
	sent := &Query{
		Nonce:  astral.NewNonce(),
		Buffer: 4096,
		Query:  astral.String16(strings.Repeat("q", 65535)),
	}

	ch := channel.New(&bytes.Buffer{})
	if err := ch.Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	obj, err := ch.Receive()
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	got, ok := obj.(*Query)
	if !ok {
		t.Fatalf("Receive returned %T; want *Query", obj)
	}
	if got.Nonce != sent.Nonce || got.Buffer != sent.Buffer || got.Query != sent.Query {
		t.Fatalf("received query (nonce %v, buffer %d, %d bytes); want (nonce %v, buffer %d, %d bytes)",
			got.Nonce, got.Buffer, len(got.Query), sent.Nonce, sent.Buffer, len(sent.Query))
	}
}

func TestFrameReadFromTruncatedInput(t *testing.T) {
	cases := []struct {
		name    string
		encoded Frame
		keep    int
		decoded Frame
		wantN   int64
		wantErr error
	}{
		{"ping cut after the nonce", &Ping{Nonce: astral.NewNonce(), Pong: true}, 8, &Ping{}, 8, io.EOF},
		{"response cut inside the buffer", &Response{Nonce: astral.NewNonce(), ErrCode: 1, Buffer: 10}, 10, &Response{}, 9, io.ErrUnexpectedEOF},
		{"query shorter than its declared length", &Query{Nonce: astral.NewNonce(), Query: "abcdefghij"}, 17, &Query{}, 17, io.ErrUnexpectedEOF},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := tc.encoded.WriteTo(&buf); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			n, err := tc.decoded.ReadFrom(bytes.NewReader(buf.Bytes()[:tc.keep]))
			if n != tc.wantN || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadFrom(%d of %d bytes) = (%d, %v); want (%d, %v)", tc.keep, buf.Len(), n, err, tc.wantN, tc.wantErr)
			}
		})
	}
}

func TestFrameString(t *testing.T) {
	nonce := astral.NewNonce()

	cases := []struct {
		name  string
		frame Frame
		want  string
	}{
		{"ping", &Ping{Nonce: nonce}, "ping(" + nonce.String() + ")"},
		{"pong", &Ping{Nonce: nonce, Pong: true}, "pong(" + nonce.String() + ")"},
		{"data", &Data{Nonce: nonce, Payload: []byte("payload")}, "data(" + nonce.String() + ",[7])"},
		{"read", &Read{Nonce: nonce, Len: 4096}, "read(" + nonce.String() + ",4096)"},
		{"response", &Response{Nonce: nonce, ErrCode: 1}, "response(" + nonce.String() + ", 1)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.frame.String(); got != tc.want {
				t.Fatalf("String() = %q; want %q", got, tc.want)
			}
		})
	}
}
