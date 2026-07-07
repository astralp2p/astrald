package nat

import (
	"encoding/json"
	"io"
	"net"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Hole represents a pair of connected endpoints resulting from a successful nat.punch operation.
type Hole struct {
	Nonce           astral.Nonce
	ActiveIdentity  *astral.Identity
	ActiveEndpoint  Endpoint
	PassiveIdentity *astral.Identity
	PassiveEndpoint Endpoint
	CreatedAt       astral.Time
}

// ObjectType implements astral.Object.
func (h Hole) ObjectType() string {
	return "nat.hole"
}

func (h Hole) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&h).WriteTo(w)
}

func (h *Hole) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(h).ReadFrom(r)
}

// MarshalJSON encodes Hole into JSON.
func (h Hole) MarshalJSON() ([]byte, error) {
	type alias Hole
	return json.Marshal(alias(h))
}

// UnmarshalJSON decodes Hole from JSON.
func (h *Hole) UnmarshalJSON(bytes []byte) error {
	type alias Hole
	var a alias
	if err := json.Unmarshal(bytes, &a); err != nil {
		return err
	}
	*h = Hole(a)
	return nil
}

// RemoteIdentity returns the identity of the remote peer.
func (h *Hole) RemoteIdentity(self *astral.Identity) (*astral.Identity, bool) {
	switch {
	case h.ActiveIdentity != nil && h.ActiveIdentity.IsEqual(self):
		return h.PassiveIdentity, true
	case h.PassiveIdentity != nil && h.PassiveIdentity.IsEqual(self):
		return h.ActiveIdentity, true
	default:
		return nil, false
	}
}

// GetLocalAddr returns the local UDP address for this hole.
func (h *Hole) GetLocalAddr(self *astral.Identity) *net.UDPAddr {
	if h.ActiveIdentity.IsEqual(self) {
		return h.ActiveEndpoint.UDPAddr()
	}
	return h.PassiveEndpoint.UDPAddr()
}

// GetRemoteAddr returns the remote UDP address for this hole.
func (h *Hole) GetRemoteAddr(self *astral.Identity) *net.UDPAddr {
	if h.ActiveIdentity.IsEqual(self) {
		return h.PassiveEndpoint.UDPAddr()
	}
	return h.ActiveEndpoint.UDPAddr()
}

func (h *Hole) MatchesPeer(peer *astral.Identity) bool {
	return h.ActiveIdentity.IsEqual(peer) || h.PassiveIdentity.IsEqual(peer)
}

func init() {
	_ = astral.Add(&Hole{})
}
