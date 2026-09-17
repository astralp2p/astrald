package gateway

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

func newParserModule(aliases map[string]*astral.Identity) *Module {
	return &Module{Deps: Deps{Dir: &namingDir{aliases: aliases}}}
}

func TestParseRejectsOtherNetworks(t *testing.T) {
	mod := newParserModule(nil)
	gw, target := astral.GenerateIdentity(), astral.GenerateIdentity()

	ep, err := mod.Parse("tcp", gw.String()+":"+target.String())
	if ep != nil || !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Fatalf("Parse(tcp) = (%v, %v); want (nil, %v)", ep, err, exonetmod.ErrUnsupportedNetwork)
	}
}

func TestParseResolvesBothIdentities(t *testing.T) {
	gw, target := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := newParserModule(map[string]*astral.Identity{"peer": gw})

	cases := []struct {
		name    string
		address string
	}{
		{"hex identities", gw.String() + ":" + target.String()},
		{"gateway alias", "peer:" + target.String()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := mod.Parse(NetworkName, tc.address)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v; want nil", tc.address, err)
			}
			got, ok := ep.(*gateway.Endpoint)
			if !ok {
				t.Fatalf("Parse(%q) returned %T; want *gateway.Endpoint", tc.address, ep)
			}
			if !got.GatewayID.IsEqual(gw) || !got.TargetID.IsEqual(target) {
				t.Fatalf("Parse(%q) = %v:%v; want %v:%v", tc.address, got.GatewayID, got.TargetID, gw, target)
			}
		})
	}
}

func TestParseRejectsMalformedAddresses(t *testing.T) {
	id := astral.GenerateIdentity()
	mod := newParserModule(nil)

	cases := []struct {
		name    string
		address string
		wantErr string
	}{
		{"no separator", "nocolon", "invalid endpoint: nocolon"},
		{"gateway equals target", id.String() + ":" + id.String(), "invalid endpoint"},
		{"unresolvable gateway", "unknown:" + id.String(), "unknown identity: unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := mod.Parse(NetworkName, tc.address)
			if ep != nil || err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Parse(%q) = (%v, %v); want (nil, %q)", tc.address, ep, err, tc.wantErr)
			}
		})
	}
}
