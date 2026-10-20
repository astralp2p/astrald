package ether

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	ether "github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// identityNode is an astral.Node that answers Identity only.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// recordingSigner records the hash it signs and returns a fixed signature.
type recordingSigner struct {
	hash []byte
}

func (s *recordingSigner) SignHash(_ *astral.Context, hash []byte) (*crypto.Signature, error) {
	s.hash = append([]byte(nil), hash...)
	return &crypto.Signature{Scheme: "test", Data: []byte{1}}, nil
}

// nodeSignerCrypto is a crypto module that answers NodeSigner only.
type nodeSignerCrypto struct {
	cryptomod.Module
	signer *recordingSigner
}

func (c *nodeSignerCrypto) NodeSigner() cryptomod.HashSigner { return c.signer }

// TestMakePacketSourceIsTheNodeIdentity pins the ether signing rule: the node
// signer signs every packet, so Source is always the node's identity.
func TestMakePacketSourceIsTheNodeIdentity(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	signer := &recordingSigner{}
	mod := &Module{
		node: &identityNode{id: nodeID},
		ctx:  astral.NewContext(nil),
	}
	mod.Crypto = &nodeSignerCrypto{signer: signer}

	data, err := mod.makePacket(astral.NewString8("ping"))
	if err != nil {
		t.Fatalf("makePacket: %v", err)
	}

	var signed ether.SignedBroadcast
	if _, err := signed.ReadFrom(bytes.NewReader(data)); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if !signed.Source.IsEqual(nodeID) {
		t.Errorf("Source = %v; want node identity %v", signed.Source, nodeID)
	}
	if !bytes.Equal(signer.hash, signed.Hash()) {
		t.Errorf("node signer signed %x; want the broadcast hash %x", signer.hash, signed.Hash())
	}
	if signed.Signature == nil {
		t.Error("Signature = nil; want the node signer's signature")
	}
}
