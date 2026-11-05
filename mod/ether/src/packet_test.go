package ether

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	ethermod "github.com/astralp2p/astrald/mod/ether"
)

// encodeEnvelope returns the bytes of an Envelope holding obj, without the send cap.
func encodeEnvelope(t *testing.T, obj astral.Object) []byte {
	t.Helper()

	var buf bytes.Buffer
	if _, err := (ether.Envelope{Version: ether.EnvelopeVersion, Object: obj}).WriteTo(&buf); err != nil {
		t.Fatalf("Envelope.WriteTo: %v", err)
	}
	return buf.Bytes()
}

// fillerOfSize returns an object whose Envelope is exactly size bytes.
func fillerOfSize(t *testing.T, size int) astral.Object {
	t.Helper()

	overhead := len(encodeEnvelope(t, astral.NewString16("")))
	filler := astral.NewString16(strings.Repeat("x", size-overhead))
	if got := len(encodeEnvelope(t, filler)); got != size {
		t.Fatalf("filler Envelope = %v bytes; want %v", got, size)
	}
	return filler
}

func mustPacket(t *testing.T, obj astral.Object) []byte {
	t.Helper()

	data, err := makePacket(obj)
	if err != nil {
		t.Fatalf("makePacket: %v", err)
	}
	return data
}

// TestMakePacketIsOneEnvelope pins the datagram format: exactly the bytes of one current Envelope.
func TestMakePacketIsOneEnvelope(t *testing.T) {
	bundle := astral.NewBundle()
	if err := bundle.Append(astral.NewString8("a"), astral.NewString16("b")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	for _, obj := range []astral.Object{astral.NewString8("ping"), bundle} {
		data := mustPacket(t, obj)
		if data[0] != ether.EnvelopeVersion {
			t.Errorf("first byte = %#x; want the Envelope version %#x", data[0], ether.EnvelopeVersion)
		}
		if !bytes.Equal(data, encodeEnvelope(t, obj)) {
			t.Errorf("makePacket(%v) = %x; want the Envelope bytes %x", obj.ObjectType(), data, encodeEnvelope(t, obj))
		}

		got, err := decodeEnvelope(data)
		if err != nil {
			t.Fatalf("decodeEnvelope: %v", err)
		}
		if !bytes.Equal(encodeEnvelope(t, got), data) {
			t.Errorf("round trip of %v changed the object: %#v", obj.ObjectType(), got)
		}
	}
}

func TestMakePacketCap(t *testing.T) {
	if _, err := makePacket(fillerOfSize(t, maxDatagramSize)); err != nil {
		t.Errorf("makePacket at the cap = %v; want nil", err)
	}

	_, err := makePacket(fillerOfSize(t, maxDatagramSize+1))
	if !errors.Is(err, ethermod.ErrPacketTooLarge) {
		t.Errorf("makePacket over the cap = %v; want %v", err, ethermod.ErrPacketTooLarge)
	}
}

// TestPushRefusesOversize pins that an oversize object is refused before any send.
func TestPushRefusesOversize(t *testing.T) {
	stubNetInterfaces(t, []NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast,
		Addrs: staticAddrs("192.168.1.10/24"),
	}}, nil)
	big := fillerOfSize(t, maxDatagramSize+1)

	// note: the socket is nil, so a send attempt would fail with "socket not initialized" instead.
	if err := (&Module{}).Push(big); !errors.Is(err, ethermod.ErrPacketTooLarge) {
		t.Errorf("Push(oversize) = %v; want %v", err, ethermod.ErrPacketTooLarge)
	}

	mod := &Module{lookupPrefixes: staticLookup(t, "192.168.1.10/24")}
	send := func(ip.IP, []byte) (int, error) {
		t.Error("pushToSubnetOf sent an oversize packet")
		return 0, nil
	}
	err := mod.pushToSubnetOf(ip.IP(net.ParseIP("192.168.1.20")), big, send)
	if !errors.Is(err, ethermod.ErrPacketTooLarge) {
		t.Errorf("pushToSubnetOf(oversize) = %v; want %v", err, ethermod.ErrPacketTooLarge)
	}
}
