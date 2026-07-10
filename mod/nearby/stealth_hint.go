package nearby

import (
	"crypto/sha256"
	"encoding/binary"
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// StealthHint is broadcast in stealth mode. Only nodes that know the userID can verify the
// commitment and recover the nodeID from the masked identity.
type StealthHint struct {
	Commitment []byte
	MaskedID   []byte
	Nonce      astral.Nonce
}

func (*StealthHint) ObjectType() string { return "mod.nearby.stealth_hint" }

func (s StealthHint) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *StealthHint) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(s).ReadFrom(r)
}

// ComputeCommitment computes sha256(sha256(userID) + nonce))
func ComputeCommitment(userID *astral.Identity, nonce astral.Nonce) []byte {
	h1 := sha256.Sum256(userID.PublicKey().SerializeCompressed())

	var nonceBytes [8]byte
	binary.LittleEndian.PutUint64(nonceBytes[:], uint64(nonce))
	h2 := sha256.Sum256(append(h1[:], nonceBytes[:]...))

	return h2[:]
}

// MaskIdentity XORs nodeID and userID compressed public keys.
func MaskIdentity(nodeID, userID *astral.Identity) []byte {
	nodeBytes := nodeID.PublicKey().SerializeCompressed()
	userBytes := userID.PublicKey().SerializeCompressed()

	masked := make([]byte, 33)

	for i := range nodeBytes {
		masked[i] = nodeBytes[i] ^ userBytes[i]
	}

	return masked
}

// UnmaskIdentity recovers a node identity by XORing the masked bytes with the known userID.
func UnmaskIdentity(masked []byte, userID *astral.Identity) (*astral.Identity, error) {
	userBytes := userID.PublicKey().SerializeCompressed()

	var raw [33]byte
	for i := range raw {
		raw[i] = masked[i] ^ userBytes[i]
	}

	key, err := secp256k1.ParsePubKey(raw[:])
	if err != nil {
		return nil, err
	}

	return astral.IdentityFromPubKey(key), nil
}

func init() {
	_ = astral.Add(&StealthHint{})
}
