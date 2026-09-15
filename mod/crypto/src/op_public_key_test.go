package crypto

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// publicKeyRig drives crypto.public_key over a pipe: send writes objects into
// the op, receive reads the op's answers back, and reported holds the op's own
// outcome, which names a panic the routing layer turns into a closed connection.
type publicKeyRig struct {
	send     channel.Sender
	receive  channel.Receiver
	reported chan error
	in       io.WriteCloser
}

// startPublicKey routes one crypto.public_key query against a bare Module.
//
// why: the op reads no module state, so no dependency has to be stubbed; the
// pipe is unbuffered, so every receive below observes one send.
func startPublicKey(t *testing.T) *publicKeyRig {
	t.Helper()

	mod := &Module{}

	op, err := routing.NewOp(mod.OpPublicKey)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	reported := make(chan error, 1)
	op.LogFunc = func(r *routing.Report) { reported <- r.Err }

	outReader, outWriter := io.Pipe()
	caller := astral.GenerateIdentity()
	q := astral.Launch(query.New(caller, caller, "crypto.public_key", nil))

	in, err := op.RouteQuery(astral.NewContext(nil), q, outWriter)
	if err != nil {
		t.Fatalf("route crypto.public_key: %v", err)
	}

	t.Cleanup(func() {
		in.Close()
		outReader.Close()
	})

	return &publicKeyRig{
		send:     channel.NewSender(in),
		receive:  channel.NewReceiver(outReader),
		reported: reported,
		in:       in,
	}
}

// receiveOne reads one object from the op, failing the test on a stalled or
// broken stream. A broken stream names the op's outcome, so a panic is
// reported as a panic rather than as a bare EOF.
func (rig *publicKeyRig) receiveOne(t *testing.T) astral.Object {
	t.Helper()

	type result struct {
		object astral.Object
		err    error
	}
	done := make(chan result, 1)
	go func() {
		object, err := rig.receive.Receive()
		done <- result{object: object, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("receive: %v (op returned: %v)", r.err, rig.opError())
			return nil
		}
		return r.object
	case <-time.After(10 * time.Second):
		t.Fatal("crypto.public_key sent nothing")
		return nil
	}
}

// opError reports the op's own outcome. A missing outcome is an op that never
// returned.
func (rig *publicKeyRig) opError() error {
	select {
	case err := <-rig.reported:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("op did not return")
	}
}

// TestPublicKeyAnswersUnsupportedKeyType: a private key of a type no engine
// supports is answered with an error_message, and the batch survives it.
//
// why: secp256k1.PublicKey answers such a key with a nil sentinel. Sent
// unchecked, the nil panicked the op, and the recovered panic closed the
// connection with no diagnostic: the caller saw EOF.
func TestPublicKeyAnswersUnsupportedKeyType(t *testing.T) {
	rig := startPublicKey(t)

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

	rig.in.Close()

	if err = rig.opError(); err != nil {
		t.Fatalf("crypto.public_key returned: %v", err)
	}
}
