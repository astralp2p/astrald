package exonet

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type parseCall struct {
	network, address string
}

type fakeParser struct {
	endpoint exonet.Endpoint
	err      error
	calls    []parseCall
}

func (p *fakeParser) Parse(network string, address string) (exonet.Endpoint, error) {
	p.calls = append(p.calls, parseCall{network, address})
	return p.endpoint, p.err
}

type fakeUnpacker struct {
	endpoint exonet.Endpoint
	networks []string
}

func (u *fakeUnpacker) Unpack(network string, _ []byte) (exonet.Endpoint, error) {
	u.networks = append(u.networks, network)
	return u.endpoint, nil
}

type fakeConn struct {
	exonetmod.Conn
}

type fakeDialer struct {
	conn      exonetmod.Conn
	endpoints []exonet.Endpoint
}

func (d *fakeDialer) Dial(_ *astral.Context, endpoint exonet.Endpoint) (exonetmod.Conn, error) {
	d.endpoints = append(d.endpoints, endpoint)
	return d.conn, nil
}

func TestModuleWithoutHandlersRejectsEveryNetwork(t *testing.T) {
	mod := &Module{}

	if _, err := mod.Parse("tcp", "x"); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Parse(tcp) error = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
	if _, err := mod.Unpack("tcp", nil); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Unpack(tcp) error = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
	if _, err := mod.Dial(astral.NewContext(nil), &tcp.Endpoint{}); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Dial(tcp endpoint) error = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
}

func TestParseDispatchesToRegisteredParser(t *testing.T) {
	mod := &Module{}
	parsed, parseErr := &tcp.Endpoint{Port: 1}, errors.New("parser error")
	p1 := &fakeParser{endpoint: parsed, err: parseErr}
	mod.SetParser("tcp", p1)

	ep, err := mod.Parse("tcp", "192.0.2.1:1")
	if ep != parsed || !errors.Is(err, parseErr) {
		t.Fatalf("Parse(tcp) = (%v, %v); want the parser's (%v, %v)", ep, err, parsed, parseErr)
	}
	want := parseCall{network: "tcp", address: "192.0.2.1:1"}
	if len(p1.calls) != 1 || p1.calls[0] != want {
		t.Fatalf("parser calls = %v; want [%v]", p1.calls, want)
	}

	if _, err := mod.Parse("kcp", "192.0.2.1:1"); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Fatalf("Parse(kcp) error = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
	if len(p1.calls) != 1 {
		t.Fatalf("tcp parser called %d times after Parse(kcp); want 1", len(p1.calls))
	}
}

func TestSetParserReplacesEarlierParser(t *testing.T) {
	mod := &Module{}
	p1, p2 := &fakeParser{}, &fakeParser{endpoint: &tcp.Endpoint{Port: 2}}
	mod.SetParser("tcp", p1)
	mod.SetParser("tcp", p2)

	ep, err := mod.Parse("tcp", "192.0.2.1:2")
	if err != nil || ep != p2.endpoint {
		t.Fatalf("Parse(tcp) = (%v, %v); want the replacement parser's endpoint %v", ep, err, p2.endpoint)
	}
	if len(p1.calls) != 0 || len(p2.calls) != 1 {
		t.Fatalf("calls: replaced parser %d, replacement %d; want 0 and 1", len(p1.calls), len(p2.calls))
	}
}

func TestUnpackDispatchesByNetworkName(t *testing.T) {
	mod := &Module{}
	tcpUnpacker := &fakeUnpacker{endpoint: &tcp.Endpoint{}}
	kcpUnpacker := &fakeUnpacker{endpoint: &kcp.Endpoint{}}
	mod.SetUnpacker("tcp", tcpUnpacker)
	mod.SetUnpacker("kcp", kcpUnpacker)

	ep, err := mod.Unpack("kcp", []byte{1})
	if err != nil || ep != kcpUnpacker.endpoint {
		t.Fatalf("Unpack(kcp) = (%v, %v); want the kcp unpacker's endpoint", ep, err)
	}
	if len(kcpUnpacker.networks) != 1 || kcpUnpacker.networks[0] != "kcp" || len(tcpUnpacker.networks) != 0 {
		t.Fatalf("unpacker calls: kcp %v, tcp %v; want [kcp] and none", kcpUnpacker.networks, tcpUnpacker.networks)
	}
}

func TestDialDispatchesByEndpointNetwork(t *testing.T) {
	mod := &Module{}
	tcpDialer := &fakeDialer{conn: &fakeConn{}}
	kcpDialer := &fakeDialer{conn: &fakeConn{}}
	mod.SetDialer("tcp", tcpDialer)
	mod.SetDialer("kcp", kcpDialer)

	target := &tcp.Endpoint{Port: 1791}
	conn, err := mod.Dial(astral.NewContext(nil), target)
	if err != nil || conn != tcpDialer.conn {
		t.Fatalf("Dial(tcp endpoint) = (%v, %v); want the tcp dialer's conn", conn, err)
	}
	if len(tcpDialer.endpoints) != 1 || tcpDialer.endpoints[0] != target || len(kcpDialer.endpoints) != 0 {
		t.Fatalf("dialer calls: tcp %v, kcp %v; want [%v] and none", tcpDialer.endpoints, kcpDialer.endpoints, target)
	}
}
