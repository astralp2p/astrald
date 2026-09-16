package crypto

import (
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

// TestPublicKeyAnswersUnsupportedKeyType: a private key of a type no engine
// supports is answered with an error_message, and the batch survives it.
//
// why: secp256k1.PublicKey answers such a key with a nil sentinel. Sent
// unchecked, the nil panicked the op, and the recovered panic closed the
// connection with no diagnostic: the caller saw EOF.
func TestPublicKeyAnswersUnsupportedKeyType(t *testing.T) {
	mod := &Module{}
	rig := startOp(t, "crypto.public_key", mod.OpPublicKey)

	err := rig.send.Send(&crypto.PrivateKey{Type: "ed25519", Key: []byte{1, 2, 3}})
	if err != nil {
		t.Fatalf("send private key: %v", err)
	}

	switch object := rig.receiveOne(t).(type) {
	case *astral.ErrorMessage:
		if object.Error() != "unsupported key type: ed25519" {
			t.Fatalf("error_message is %q; want the key type", object.Error())
		}
	default:
		t.Fatalf("answer is %v; want error_message", object.ObjectType())
	}

	// the batch stays alive: a supported key still derives after the failed item
	err = rig.send.Send(secp256k1.New())
	if err != nil {
		t.Fatalf("send secp256k1 key: %v", err)
	}

	switch object := rig.receiveOne(t).(type) {
	case *crypto.PublicKey:
		if object.Type != secp256k1.KeyType {
			t.Fatalf("derived key type is %v; want %v", object.Type, secp256k1.KeyType)
		}
	default:
		t.Fatalf("answer is %v; want mod.crypto.public_key", object.ObjectType())
	}

	err = rig.send.Send(&astral.EOS{})
	if err != nil {
		t.Fatalf("send eos: %v", err)
	}

	if object := rig.receiveOne(t); object.ObjectType() != (&astral.EOS{}).ObjectType() {
		t.Fatalf("answer to eos is %v; want eos", object.ObjectType())
	}

	rig.raw.Close()

	if err = rig.opError(); err != nil {
		t.Fatalf("crypto.public_key returned: %v", err)
	}
}
