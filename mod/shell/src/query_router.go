package shell

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if !q.Target.IsEqual(mod.node.Identity()) {
		return query.RouteNotFound()
	}

	// why here: this router is the sole mount point for every module's op
	// router (deps.go), so one test guards all of them, and an op added later
	// is guarded the day it is written.
	//
	// why Reject and not RouteNotFound: PriorityRouter stops on ErrRejected, so
	// the caller reads a refusal rather than a missing route.
	//
	// why the network origin is not refused here: a link query reaching another
	// module's op is ordinary inter-node traffic, and this router is the only
	// mount point it has. Admission to shell.shell is guarded at that op
	// instead, where it bounds one op rather than every scope.
	if q.IsMCP() {
		return query.Reject()
	}

	return mod.scopes.RouteQuery(ctx, q, w)
}
