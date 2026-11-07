package user

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	ethermod "github.com/astralp2p/astrald/mod/ether"
	nearbysrc "github.com/astralp2p/astrald/mod/nearby/src"
	secp256k1mod "github.com/astralp2p/astrald/mod/secp256k1"
	"github.com/astralp2p/astrald/resources"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// noAssets is a node without config files.
type noAssets struct {
	assets.Assets
}

func (noAssets) LoadYAML(string, any) error { return resources.ErrNotFound }

// keyCrypto signs as the node with key.
type keyCrypto struct {
	cryptomod.Module
	key *crypto.PrivateKey
}

func (c keyCrypto) NodeSigner() cryptomod.HashSigner { return secp256k1mod.NewHashSignerASN1(c.key) }

// pushEther records every object pushed.
type pushEther struct {
	ethermod.Module

	mu     sync.Mutex
	pushed []astral.Object
}

func (e *pushEther) Push(obj astral.Object) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pushed = append(e.pushed, obj)
	return nil
}

// stealthDatagram returns the datagram that a node of user, with node key key,
// broadcasts with a stored mode left unset: the user module composes the status,
// the nearby module signs it, and the ether Envelope carries it.
func stealthDatagram(t *testing.T, key *crypto.PrivateKey, userID *astral.Identity) []byte {
	t.Helper()

	nodeID := secp256k1.Identity(secp256k1.PublicKey(key))
	node := &identityNode{id: nodeID}
	userMod := &Module{node: node, log: log.New(nodeID)}
	signed := &auth.SignedContract{Contract: &auth.Contract{Issuer: userID, Subject: nodeID}}
	if err := userMod.config.ActiveContract.Set(nil, signed); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	loaded, err := nearbysrc.Loader{}.Load(node, noAssets{}, log.New(nodeID))
	if err != nil {
		t.Fatalf("load nearby: %v", err)
	}
	nearbyMod := loaded.(*nearbysrc.Module)
	pushed := &pushEther{}
	nearbyMod.Deps = nearbysrc.Deps{Crypto: keyCrypto{key: key}, Ether: pushed, User: userMod}
	userMod.Nearby = nearbyMod
	nearbyMod.AddStatusComposer(userMod)

	if err := nearbyMod.Broadcast(); err != nil || len(pushed.pushed) != 1 {
		t.Fatalf("Broadcast = %v, pushed %v objects; want one status", err, len(pushed.pushed))
	}
	var buf bytes.Buffer
	if _, err := (&ether.Envelope{Version: ether.EnvelopeVersion, Object: pushed.pushed[0]}).WriteTo(&buf); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	return buf.Bytes()
}

// recoversKey reports whether ECDSA public key recovery from the DER signature
// sig over hash yields want for any recovery id.
func recoversKey(sig, hash []byte, want *astral.Identity) bool {
	parsed, err := ecdsa.ParseDERSignature(sig)
	if err != nil {
		return false
	}

	r, s := parsed.R(), parsed.S()
	compact := make([]byte, 65)
	r.PutBytesUnchecked(compact[1:33])
	s.PutBytesUnchecked(compact[33:65])
	for id := byte(0); id < 4; id++ {
		compact[0] = 27 + 4 + id
		key, _, err := ecdsa.RecoverCompact(compact, hash)
		if err == nil && astral.IdentityFromPubKey(key).IsEqual(want) {
			return true
		}
	}
	return false
}

// TestStealthDatagramHidesTheNodeAndTheUser: the datagram of a Stealth status
// holds a StealthHint and the StatusProof only, holds neither the node's nor
// the user's 33 key bytes nor their hashes, and its signature recovers no key;
// unwrapped, the same signature recovers the node.
func TestStealthDatagramHidesTheNodeAndTheUser(t *testing.T) {
	nodeKey, userID := secp256k1.New(), secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
	nodeID := secp256k1.Identity(secp256k1.PublicKey(nodeKey))
	data := stealthDatagram(t, nodeKey, userID)

	var env ether.Envelope
	if _, err := env.ReadFrom(bytes.NewReader(data)); err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	status, ok := env.Object.(*nearby.StatusMessage)
	if !ok {
		t.Fatalf("datagram carries %v; want a status", env.Object.ObjectType())
	}
	objs := status.Attachments.Objects()
	if len(objs) != 2 || objs[0].ObjectType() != (&nearby.StealthHint{}).ObjectType() || objs[1].ObjectType() != (&nearby.StatusProof{}).ObjectType() {
		t.Fatalf("status holds %v; want a stealth_hint and a status_proof", objs)
	}

	for name, id := range map[string]*astral.Identity{"node": nodeID, "user": userID} {
		key := id.PublicKey().SerializeCompressed()
		hash := sha256.Sum256(key)
		if bytes.Contains(data, key) || bytes.Contains(data, hash[:]) {
			t.Errorf("the datagram holds the %v key or its hash", name)
		}
	}

	others, proof, _ := nearby.SplitProof(objs)
	hash, err := nearby.ProofHash(others, proof.Nonce, proof.Timestamp)
	if err != nil {
		t.Fatalf("ProofHash: %v", err)
	}
	if recoversKey(proof.Sig, hash, nodeID) {
		t.Error("the wrapped signature recovers the node key")
	}
	if !recoversKey(nearby.WrapProofSig(nodeID, proof.Nonce, proof.Sig), hash, nodeID) {
		t.Error("the unwrapped signature does not recover the node key; the recovery check is void")
	}
}
