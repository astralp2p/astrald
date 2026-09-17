package gateway

import (
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

func TestUnpackRoundTripsPack(t *testing.T) {
	ep := gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity())

	got, err := Unpack(ep.Pack())
	if err != nil {
		t.Fatalf("Unpack(Pack()) error = %v; want nil", err)
	}
	if got.Address() != ep.Address() {
		t.Fatalf("Unpack(Pack()).Address() = %q; want %q", got.Address(), ep.Address())
	}

	viaModule, err := (&Module{}).Unpack(NetworkName, ep.Pack())
	if err != nil || viaModule == nil || viaModule.Address() != ep.Address() {
		t.Fatalf("Module.Unpack(gw) = (%v, %v); want address %q", viaModule, err, ep.Address())
	}
}

func TestModuleUnpackRejectsOtherNetworks(t *testing.T) {
	ep := gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity())

	got, err := (&Module{}).Unpack("tcp", ep.Pack())
	if got != nil || !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Fatalf("Module.Unpack(tcp) = (%v, %v); want (nil, %v)", got, err, exonetmod.ErrUnsupportedNetwork)
	}
}

func TestUnpackTruncatedData(t *testing.T) {
	data := gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity()).Pack()

	if _, err := Unpack(data[:10]); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Unpack(first 10 of %d bytes) error = %v; want %v", len(data), err, io.ErrUnexpectedEOF)
	}
}
