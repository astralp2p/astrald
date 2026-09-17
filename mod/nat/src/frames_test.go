package nat

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestPingFrameWireLayout(t *testing.T) {
	tests := []struct {
		name  string
		frame pingFrame
		wire  []byte
	}{
		{"pong", pingFrame{Nonce: 0x0102030405060708, Pong: true}, []byte{1, 2, 3, 4, 5, 6, 7, 8, 1}},
		{"ping", pingFrame{Nonce: 0x0102030405060708, Pong: false}, []byte{1, 2, 3, 4, 5, 6, 7, 8, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := tt.frame.WriteTo(&buf)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if n != 9 {
				t.Errorf("WriteTo n = %d; want 9", n)
			}
			if !bytes.Equal(buf.Bytes(), tt.wire) {
				t.Errorf("wire = % x; want % x", buf.Bytes(), tt.wire)
			}

			var got pingFrame
			n, err = got.ReadFrom(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("ReadFrom: %v", err)
			}
			if n != 9 {
				t.Errorf("ReadFrom n = %d; want 9", n)
			}
			if got != tt.frame {
				t.Errorf("decoded %+v; want %+v", got, tt.frame)
			}
		})
	}
}

func TestPingFramePongByteOtherThanOneIsPing(t *testing.T) {
	var f pingFrame
	if _, err := f.ReadFrom(bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 1, 2})); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if f.Pong {
		t.Error("Pong = true for byte 0x02; want false")
	}
}

func TestPingFrameReadShortInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"truncated nonce", []byte{1, 2, 3}, io.ErrUnexpectedEOF},
		{"empty", nil, io.EOF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f pingFrame
			if _, err := f.ReadFrom(bytes.NewReader(tt.input)); !errors.Is(err, tt.want) {
				t.Errorf("ReadFrom(% x) = %v; want %v", tt.input, err, tt.want)
			}
		})
	}
}
