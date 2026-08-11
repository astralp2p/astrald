package nodes

import (
	"reflect"

	"github.com/astralp2p/astral-go/api/exonet"
)

// knownEndpoint returns ep, or nil when it names no address.
//
// why: LinkInfo's endpoint fields are interface-typed, so nil travels as a
// zero-length type tag and reads back as "not known", which is the truth about
// a link whose local side has no address to report yet. A zero VALUE instead
// claims to be an address and is not one.
//
// The claim is also unreadable. astral-go writes a tor digest as whatever
// bytes it holds and reads it as exactly DigestSize, so an empty digest
// serialises two bytes of payload where the reader wants thirty-seven and
// shifts every field after it — the receiver fails to decode the whole record,
// not just the endpoint. An inbound Tor link reaches exactly that state, and
// nodes.new_link answered one: astral-py raised `ShortRead: wanted 105 bytes
// at offset 131, 32 available`, reproduced byte-for-byte off a hand-encoded
// LinkInfo carrying `&tor.Endpoint{}`.
//
// note: the codec asymmetry is astral-go's and is worth fixing there too — a
// digest's wire length should not depend on its value. This keeps astrald from
// putting an unreadable record on the wire in the meantime, and states
// something true either way.
func knownEndpoint(ep exonet.Endpoint) exonet.Endpoint {
	if ep == nil {
		return nil
	}

	// a non-nil interface can still hold a nil pointer, and IsZero below has a
	// value receiver — calling it on one would panic rather than answer.
	if v := reflect.ValueOf(ep); v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}

	if z, ok := ep.(interface{ IsZero() bool }); ok && z.IsZero() {
		return nil
	}

	return ep
}
