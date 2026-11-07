package ether

import (
	"bytes"
	"errors"
	"fmt"
	"net"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	"golang.org/x/net/ipv4"
)

// The reasons a received datagram is dropped.
var (
	errOversize      = errors.New("datagram over the size cap")
	errNotIPv4       = errors.New("source is not IPv4")
	errNotLinkTTL    = errors.New("datagram did not arrive with the link TTL")
	errOwnSource     = errors.New("source is a local address")
	errOffLink       = errors.New("source is outside every local subnet")
	errVersion       = errors.New("unknown envelope version")
	errDecode        = errors.New("envelope does not decode")
	errTrailingBytes = errors.New("bytes follow the envelope")
)

// datagram is one received UDP payload with what the socket reported about it.
type datagram struct {
	data   []byte
	src    net.IP
	ttl    int
	hasTTL bool // note: false when the datagram came without a TTL control message
}

func newDatagram(data []byte, cm *ipv4.ControlMessage, src net.Addr) datagram {
	d := datagram{data: data}
	if udp, ok := src.(*net.UDPAddr); ok {
		d.src = udp.IP
	}
	if cm != nil {
		d.ttl, d.hasTTL = cm.TTL, true
	}

	return d
}

// receiveLoop reads datagrams until the socket closes or ctx ends.
func (mod *Module) receiveLoop(ctx *astral.Context) {
	// note: one extra byte makes an oversize datagram visible after the read truncates it.
	buf := make([]byte, maxDatagramSize+1)

	for {
		n, cm, src, err := mod.socket.ReadFrom(buf)
		if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
			return
		}
		if err != nil {
			mod.log.Logv(2, "read datagram: %v", err)
			continue
		}

		mod.receiveDatagram(newDatagram(buf[:n], cm, src))
	}
}

// receiveDatagram delivers the object of an accepted datagram to the node's own receivers.
func (mod *Module) receiveDatagram(d datagram) {
	// why: a datagram is untrusted input, so a panic while decoding or delivering it costs that datagram only.
	defer func() {
		if r := recover(); r != nil {
			mod.log.Error("dropped datagram from %v: panic: %v", d.src, r)
		}
	}()

	event, err := handleDatagram(d, mod.lookupPrefixes)
	if err != nil {
		mod.log.Logv(2, "dropped datagram from %v: %v", d.src, err)
		return
	}

	// note: Receive fails when no receiver accepts the event, which is not an ether fault.
	_ = mod.Objects.Receive(event, mod.node.Identity())
}

// handleDatagram applies the ether receive rules in order and returns the event to deliver.
// note: the object reaches receivers only inside the event, as sent by the local node.
// Spec: .ai/system/protocols/ether/README.md
func handleDatagram(d datagram, lookup prefixLookup) (*ether.EventBroadcastReceived, error) {
	src := d.src.To4()
	switch {
	case len(d.data) > maxDatagramSize:
		return nil, errOversize
	case src == nil:
		return nil, errNotIPv4
	case checkInboundTTL && (!d.hasTTL || d.ttl != linkTTL):
		return nil, errNotLinkTTL
	}

	local, err := lookup(src)
	switch {
	case err != nil:
		return nil, fmt.Errorf("find local prefixes: %w", err)
	case isOwnIP(local, src):
		return nil, errOwnSource
	case !isOnLink(local, src):
		return nil, errOffLink
	}

	obj, err := decodeEnvelope(d.data)
	if err != nil {
		return nil, err
	}

	return &ether.EventBroadcastReceived{SourceIP: ip.IP(src), Object: obj}, nil
}

// decodeEnvelope returns the object of a datagram that holds exactly one current Envelope.
func decodeEnvelope(data []byte) (astral.Object, error) {
	if len(data) == 0 || data[0] != ether.EnvelopeVersion {
		return nil, errVersion
	}

	var env ether.Envelope
	n, err := env.ReadFrom(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errDecode, err)
	}
	if n < int64(len(data)) {
		return nil, errTrailingBytes
	}

	return env.Object, nil
}
