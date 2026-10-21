package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// TestGrantOpRecordsNothingWhenConstraintsNeverArrive: a constrained grant
// whose caller closes before sending the constraint bundle fails and records
// no grant. An unconstrained permit in its place grants more than the caller
// asked for.
func TestGrantOpRecordsNothingWhenConstraintsNeverArrive(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	op, err := routing.NewOp(mod.OpGrant)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	// why: the op's report is the only signal that the op has returned; the
	// grant table is read after it, never concurrently with the op's write.
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	caller := astral.GenerateIdentity()
	q := query.New(caller, caller, "apphost.grant?identity="+app.String()+"&action="+action+"&constrained=true", nil)
	requester, err := op.RouteQuery(ctx, astral.Launch(q), newRecordingWriter())
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	requester.Close()

	select {
	case r := <-reports:
		if r.Err != nil {
			t.Fatalf("the op failed: %v", r.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("the node holds %d grants for the app; want 0", len(grants))
	}
}
