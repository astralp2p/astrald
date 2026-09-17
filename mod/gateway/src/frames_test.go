package gateway

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

func TestFramesRoundTripThroughChannel(t *testing.T) {
	cases := []struct {
		name  string
		frame astral.Object
	}{
		{"ping", &Ping{}},
		{"pong", &Ping{Pong: true}},
		{"handoff", &Handoff{}},
		{"handoff ack", &HandoffAck{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := channel.New(&bytes.Buffer{})
			if err := ch.Send(tc.frame); err != nil {
				t.Fatalf("Send(%T) error = %v; want nil", tc.frame, err)
			}

			got, err := ch.Receive()
			if err != nil {
				t.Fatalf("Receive() error = %v; want nil", err)
			}
			if !reflect.DeepEqual(got, tc.frame) {
				t.Fatalf("Receive() = %#v; want %#v", got, tc.frame)
			}
		})
	}
}
