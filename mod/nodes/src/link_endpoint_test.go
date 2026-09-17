package nodes

import (
	"testing"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
)

func TestKnownEndpointDropsUnknownAddresses(t *testing.T) {
	cases := []struct {
		name string
		ep   exonet.Endpoint
	}{
		{"nil interface", nil},
		{"nil tcp pointer", (*tcp.Endpoint)(nil)},
		{"zero tcp endpoint", &tcp.Endpoint{}},
		{"zero tor endpoint", &tor.Endpoint{}},
		{"zero gateway endpoint", &gateway.Endpoint{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := knownEndpoint(tc.ep); got != nil {
				t.Fatalf("knownEndpoint(%#v) = %#v; want nil", tc.ep, got)
			}
		})
	}
}

func TestKnownEndpointKeepsAddresses(t *testing.T) {
	cases := []struct {
		name string
		ep   exonet.Endpoint
	}{
		{"tcp endpoint", tcpEndpoint("192.0.2.1")},
		{"gateway endpoint", gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := knownEndpoint(tc.ep); got != tc.ep {
				t.Fatalf("knownEndpoint(%v) = %v; want the same endpoint", tc.ep, got)
			}
		})
	}
}
