package ether

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/crypto"
)

var _ astral.Object = &SignedBroadcast{}

type SignedBroadcast struct {
	Broadcast
	Signature *crypto.Signature
}

func (SignedBroadcast) ObjectType() string {
	return "mod.ether.signed_broadcast"
}

func (b SignedBroadcast) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&b).WriteTo(w)
}

func (b *SignedBroadcast) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(b).ReadFrom(r)
}

// Hash returns the object ID hash of the embedded Broadcast, not the full SignedBroadcast,
// so the signature covers only the payload and not the signature field itself.
func (b SignedBroadcast) Hash() []byte {
	objectID, err := astral.ResolveObjectID(&b.Broadcast)
	if err != nil {
		return nil
	}
	return objectID.Hash[:]
}

func init() {
	_ = astral.Add(&SignedBroadcast{})
}
