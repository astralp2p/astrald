package ether

import (
	"bytes"
	"fmt"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/astral"
	ethermod "github.com/astralp2p/astrald/mod/ether"
)

// maxDatagramSize caps an ether datagram on send and on receive.
// why: about twice the largest possible nearby status, 510-533 B with a 255-byte alias (astral-go api/nearby TestStatusDatagramSizes).
// why: the hard ceiling is 1472 B, the Ethernet MTU 1500 minus the IPv4 header 20 minus the UDP header 8.
const maxDatagramSize = 1024

// makePacket returns the datagram that carries obj: exactly the bytes of one Envelope.
func makePacket(obj astral.Object) ([]byte, error) {
	var buf bytes.Buffer
	env := ether.Envelope{Version: ether.EnvelopeVersion, Object: obj}
	if _, err := env.WriteTo(&buf); err != nil {
		return nil, err
	}

	if buf.Len() > maxDatagramSize {
		return nil, fmt.Errorf("%w: %v bytes over the %v-byte cap", ethermod.ErrPacketTooLarge, buf.Len(), maxDatagramSize)
	}

	return buf.Bytes(), nil
}
