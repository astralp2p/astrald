package log

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// seeNodeStateOp is one row of the SeeNodeState surface in mod/log: the op and a
// query that binds its required arguments.
type seeNodeStateOp struct {
	name string
	op   func(*Module) any
	args string
}

func seeNodeStateOps() []seeNodeStateOp {
	return []seeNodeStateOp{
		{"log.listen", func(m *Module) any { return m.OpListen }, ""},
	}
}

// TestSeeNodeStateRefusesCallerWithoutPermits is the coverage measure for the
// SeeNodeState action in mod/log: log.listen must ask before it subscribes, and
// must reject when the answer is no.
//
// note: the module is a bare struct with no logger, so an op that reached
// AddLogger past its authorization check would panic on the nil logger.
// note: the allowed path is startListen in op_listen_test.go, which holds the
// action.
func TestSeeNodeStateRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range seeNodeStateOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, len(actions))
			}

			action, ok := actions[0].(*auth.SeeNodeStateAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), (&auth.SeeNodeStateAction{}).ObjectType())
			}

			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, action.Actor(), caller)
			}
		})
	}
}
