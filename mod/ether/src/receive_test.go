package ether

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/objects"
	"golang.org/x/net/ipv4"
)

// identityNode is an astral.Node that answers Identity only.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

type receivedObject struct {
	obj    astral.Object
	source *astral.Identity
}

// recordingObjects is an objects module that answers Receive only.
// It panics on the next call while panicNext is set and records every other call.
type recordingObjects struct {
	objects.Module
	panicNext atomic.Bool
	received  chan receivedObject
}

func (o *recordingObjects) Receive(obj astral.Object, source *astral.Identity) error {
	if o.panicNext.CompareAndSwap(true, false) {
		panic("receiver panic")
	}
	o.received <- receivedObject{obj: obj, source: source}
	return nil
}

// oldSignedBroadcast returns a datagram shaped like a mod.ether.signed_broadcast sent in 2026:
// a UnixNano timestamp, the object, a 33-byte source identity and a signature.
func oldSignedBroadcast(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	stamp := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC).UnixNano()
	_ = binary.Write(&buf, binary.BigEndian, uint64(stamp))
	buf.Write(mustPacket(t, astral.NewString8("hello"))[1:])
	_, _ = astral.GenerateIdentity().WriteTo(&buf)
	buf.Write(bytes.Repeat([]byte{0x30}, 71))

	if buf.Bytes()[0] != 0x18 {
		t.Fatalf("old datagram starts with %#x; want 0x18", buf.Bytes()[0])
	}
	return buf.Bytes()
}

func linkDatagram(data []byte, src string) datagram {
	return datagram{data: data, src: net.ParseIP(src), ttl: linkTTL, hasTTL: true}
}

func withFirstByte(data []byte, b byte) []byte {
	out := append([]byte(nil), data...)
	out[0] = b
	return out
}

func TestHandleDatagram(t *testing.T) {
	packet := mustPacket(t, astral.NewString8("hello"))
	unknownType := []byte{ether.EnvelopeVersion, 8, 'm', 'o', 'd', '.', 'n', 'o', 'p', 'e'}
	errLoad := errors.New("interfaces unavailable")
	failing := func(net.IP) ([]localPrefix, error) { return nil, errLoad }
	peer := "192.168.1.20"

	tests := []struct {
		name    string
		d       datagram
		lookup  prefixLookup // note: nil looks up testLocal
		wantErr error        // note: nil means delivered
		ttlRule bool         // note: the row tests the TTL check, which Windows skips
	}{
		{name: "delivered", d: linkDatagram(packet, peer)},
		{name: "at the cap", d: linkDatagram(mustPacket(t, fillerOfSize(t, maxDatagramSize)), peer)},
		{name: "over the cap", d: linkDatagram(encodeEnvelope(t, fillerOfSize(t, maxDatagramSize+1)), peer), wantErr: errOversize},
		{name: "IPv6 source", d: linkDatagram(packet, "fe80::1"), wantErr: errNotIPv4},
		{name: "no source", d: datagram{data: packet, ttl: linkTTL, hasTTL: true}, wantErr: errNotIPv4},
		{name: "no TTL control message", d: datagram{data: packet, src: net.ParseIP(peer)}, wantErr: errNotLinkTTL, ttlRule: true},
		{name: "routed TTL", d: datagram{data: packet, src: net.ParseIP(peer), ttl: linkTTL - 1, hasTTL: true}, wantErr: errNotLinkTTL, ttlRule: true},
		{name: "own address", d: linkDatagram(packet, "192.168.1.10"), wantErr: errOwnSource},
		{name: "own address on another subnet", d: linkDatagram(packet, "10.0.0.5"), wantErr: errOwnSource},
		{name: "off link", d: linkDatagram(packet, "192.168.2.1"), wantErr: errOffLink},
		{name: "link-local source", d: linkDatagram(packet, "169.254.1.1"), wantErr: errOffLink},
		{name: "loopback source", d: linkDatagram(packet, "127.0.0.1"), wantErr: errOffLink},
		{name: "prefix lookup fails", d: linkDatagram(packet, peer), lookup: failing, wantErr: errLoad},
		{name: "empty", d: linkDatagram(nil, peer), wantErr: errVersion},
		{name: "other version", d: linkDatagram(withFirstByte(packet, 2), peer), wantErr: errVersion},
		{name: "old signed broadcast", d: linkDatagram(oldSignedBroadcast(t), peer), wantErr: errVersion},
		{name: "trailing bytes", d: linkDatagram(append(append([]byte(nil), packet...), 0), peer), wantErr: errTrailingBytes},
		{name: "truncated", d: linkDatagram(packet[:len(packet)-1], peer), wantErr: errDecode},
		{name: "unknown object type", d: linkDatagram(unknownType, peer), wantErr: errDecode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := tt.lookup
			if lookup == nil {
				lookup = staticLookup(t, testLocal...)
			}
			wantErr := tt.wantErr
			if tt.ttlRule && !checkInboundTTL {
				wantErr = nil
			}

			event, err := handleDatagram(tt.d, lookup)
			if !errors.Is(err, wantErr) {
				t.Fatalf("handleDatagram = (%v, %v); want error %v", event, err, wantErr)
			}
			if wantErr != nil {
				return
			}
			if event == nil || event.Object == nil {
				t.Fatalf("handleDatagram = %v; want an event with an object", event)
			}
			if len(event.SourceIP) != net.IPv4len || !net.IP(event.SourceIP).Equal(tt.d.src) {
				t.Errorf("SourceIP = %v (%v bytes); want %v as 4 bytes", event.SourceIP, len(event.SourceIP), tt.d.src)
			}
		})
	}
}

func TestHandleDatagramDeliversTheObject(t *testing.T) {
	event, err := handleDatagram(linkDatagram(mustPacket(t, astral.NewString8("hello")), "192.168.1.20"), staticLookup(t, testLocal...))
	if err != nil {
		t.Fatalf("handleDatagram: %v", err)
	}
	if s, ok := event.Object.(*astral.String8); !ok || *s != "hello" {
		t.Errorf("Object = %#v; want string8 \"hello\"", event.Object)
	}
}

// TestReceiveDatagramDeliversOnlyTheEvent pins that receivers get the event alone, sent by the local node.
func TestReceiveDatagramDeliversOnlyTheEvent(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	objs := &recordingObjects{received: make(chan receivedObject, 4)}
	mod := &Module{
		node:           &identityNode{id: nodeID},
		log:            &log.Logger{},
		lookupPrefixes: staticLookup(t, testLocal...),
	}
	mod.Objects = objs

	mod.receiveDatagram(linkDatagram(mustPacket(t, astral.NewString8("hello")), "192.168.1.20"))
	close(objs.received)

	var got []receivedObject
	for r := range objs.received {
		got = append(got, r)
	}
	if len(got) != 1 {
		t.Fatalf("Receive called %v times; want once", len(got))
	}
	if _, ok := got[0].obj.(*ether.EventBroadcastReceived); !ok {
		t.Errorf("delivered %T; want *ether.EventBroadcastReceived", got[0].obj)
	}
	if !got[0].source.IsEqual(nodeID) {
		t.Errorf("delivered as %v; want the node %v", got[0].source, nodeID)
	}
}

// TestReceiveLoopSurvivesAPanickingReceiver sends two datagrams over loopback with the link TTL:
// the first panics in the receiver, the second still reaches the receivers.
func TestReceiveLoopSurvivesAPanickingReceiver(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	objs := &recordingObjects{received: make(chan receivedObject, 4)}
	objs.panicNext.Store(true)
	mod := &Module{
		node:   &identityNode{id: nodeID},
		log:    &log.Logger{},
		config: Config{UDPPort: reserveUDPPort(t)},
		// note: loopback is never a local prefix in production; the test injects it to stay off the LAN.
		lookupPrefixes: loopbackLookup,
	}
	mod.Objects = objs
	if err := mod.setupSocket(); err != nil {
		t.Fatalf("setupSocket: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.receiveLoop(ctx)
	}()

	sender := linkSender(t)
	for _, text := range []string{"first", "second"} {
		target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: mod.config.UDPPort}
		if _, err := sender.WriteTo(mustPacket(t, astral.NewString8(text)), nil, target); err != nil {
			t.Fatalf("send %v: %v", text, err)
		}
	}

	select {
	case got := <-objs.received:
		event, ok := got.obj.(*ether.EventBroadcastReceived)
		if !ok {
			t.Fatalf("delivered %T; want *ether.EventBroadcastReceived", got.obj)
		}
		if s, ok := event.Object.(*astral.String8); !ok || *s != "second" {
			t.Errorf("delivered %#v; want the second datagram", event.Object)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no datagram delivered after the receiver panicked")
	}
	if objs.panicNext.Load() {
		t.Error("the receiver never panicked")
	}

	mod.socket.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("receiveLoop did not return after the socket closed")
	}
}

// loopbackLookup places the node at 127.0.0.2 on 127.0.0.0/8, so 127.0.0.1 is an on-link peer.
func loopbackLookup(net.IP) ([]localPrefix, error) {
	_, subnet, _ := net.ParseCIDR("127.0.0.0/8")
	return []localPrefix{{addr: net.IPv4(127, 0, 0, 2).To4(), subnet: subnet}}, nil
}

// linkSender returns a loopback socket that sends with the link TTL.
func linkSender(t *testing.T) *ipv4.PacketConn {
	t.Helper()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	sender := ipv4.NewPacketConn(conn)
	t.Cleanup(func() { sender.Close() })

	if err := sender.SetTTL(linkTTL); err != nil {
		t.Fatalf("SetTTL: %v", err)
	}
	return sender
}
