package crypto

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// opRig drives one crypto op over a pipe: send writes objects into the op,
// receive reads the op's answers back, and reported holds the op's own outcome,
// which names a panic the routing layer turns into a closed connection. raw
// writes bytes the sender cannot produce, such as an undecodable frame.
type opRig struct {
	name     string
	send     channel.Sender
	receive  channel.Receiver
	reported chan error
	raw      io.WriteCloser
}

// startOp routes one query named name against opFunc on a bare Module.
//
// why: the five hand-rolled ops read no module state before their channel loop,
// so no dependency has to be stubbed; the pipe is unbuffered, so every receive
// below observes one send.
func startOp(t *testing.T, name string, opFunc any) *opRig {
	t.Helper()

	op, err := routing.NewOp(opFunc)
	if err != nil {
		t.Fatalf("new op %s: %v", name, err)
	}

	reported := make(chan error, 1)
	op.LogFunc = func(r *routing.Report) { reported <- r.Err }

	outReader, outWriter := io.Pipe()
	caller := astral.GenerateIdentity()
	q := astral.Launch(query.New(caller, caller, name, nil))

	in, err := op.RouteQuery(astral.NewContext(nil), q, outWriter)
	if err != nil {
		t.Fatalf("route %s: %v", name, err)
	}

	t.Cleanup(func() {
		in.Close()
		outReader.Close()
	})

	return &opRig{
		name:     name,
		send:     channel.NewSender(in),
		receive:  channel.NewReceiver(outReader),
		reported: reported,
		raw:      in,
	}
}

// receiveOne reads one object from the op, failing the test on a stalled or
// broken stream. A broken stream names the op's outcome, so a panic is
// reported as a panic rather than as a bare EOF.
func (rig *opRig) receiveOne(t *testing.T) astral.Object {
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
		t.Fatalf("%s sent nothing", rig.name)
		return nil
	}
}

// opError reports the op's own outcome. A missing outcome is an op that never
// returned.
func (rig *opRig) opError() error {
	select {
	case err := <-rig.reported:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("op did not return")
	}
}
