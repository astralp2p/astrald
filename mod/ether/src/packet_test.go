package ether

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

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

// TestMakePacketCap pins the spec's 1024-byte datagram cap on send.
func TestMakePacketCap(t *testing.T) {
	if _, err := makePacket(fillerOfSize(t, 1024)); err != nil {
		t.Errorf("makePacket at the cap = %v; want nil", err)
	}

	_, err := makePacket(fillerOfSize(t, 1025))
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

// TestPushSendsTheEnvelope pins that Push sends the Envelope bytes of the object to the
// broadcast address of a local interface, with the link TTL 255.
func TestPushSendsTheEnvelope(t *testing.T) {
	mod := &Module{config: Config{UDPPort: reserveUDPPort(t)}}
	if err := mod.setupSocket(); err != nil {
		t.Fatalf("setupSocket: %v", err)
	}
	defer mod.socket.Close()
	// note: a /32 prefix is its own broadcast address, so the datagram comes back to the socket.
	stubNetInterfaces(t, []NetInterface{{Flags: net.FlagUp | net.FlagBroadcast, Addrs: staticAddrs("127.0.0.1/32")}}, nil)

	obj := astral.NewString8("ping")
	if err := mod.Push(obj); err != nil {
		t.Fatalf("Push: %v", err)
	}

	buf := make([]byte, 2048)
	if err := mod.socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, cm, _, err := mod.socket.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no datagram arrived: %v", err)
	}
	if !bytes.Equal(buf[:n], encodeEnvelope(t, obj)) {
		t.Errorf("sent %x; want the Envelope bytes %x", buf[:n], encodeEnvelope(t, obj))
	}
	if checkInboundTTL && (cm == nil || cm.TTL != 255) {
		t.Errorf("datagram arrived with control message %v; want TTL 255", cm)
	}
}
