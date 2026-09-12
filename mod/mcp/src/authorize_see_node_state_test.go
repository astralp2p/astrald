package mcp

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// seeNodeStateOp is one row of the SeeNodeState surface in mod/mcp: the op and a
// query that binds its required arguments.
type seeNodeStateOp struct {
	name string
	op   func(*Module) any
	args string
}

func seeNodeStateOps(agentID *astral.Identity) []seeNodeStateOp {
	return []seeNodeStateOp{
		{"mcp.agent", func(m *Module) any { return m.OpAgent }, "?id=" + agentID.String()},
	}
}

// TestSeeNodeStateRefusesCallerWithoutPermits is the coverage measure for the
// SeeNodeState action in mod/mcp: mcp.agent must ask before it reads an agent's
// record, and must reject when the answer is no.
//
// note: the module is a bare struct with no directory and no database, so an op
// that reached past its authorization check would panic on a nil field.
func TestSeeNodeStateRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range seeNodeStateOps(astral.GenerateIdentity()) {
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
