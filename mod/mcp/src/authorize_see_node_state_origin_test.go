package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// TestSeeNodeStateKeepsOriginRefusal: a query off a link is refused by origin,
// as before the guard, and the authority is never asked.
func TestSeeNodeStateKeepsOriginRefusal(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	op, err := routing.NewOp(mod.OpAgent)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	caller := astral.GenerateIdentity()
	q := astral.Launch(query.New(caller, caller, "mcp.agent?id="+caller.String(), nil))
	q.Extra.Set("origin", astral.OriginNetwork)

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	_, err = op.RouteQuery(ctx, q, w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("mcp.agent answered a network-origin query: got err %v, want a rejection", err)
	}

	if n := w.written(); n != 0 {
		t.Fatalf("mcp.agent wrote %d bytes to a network-origin query; want none", n)
	}

	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("mcp.agent asked the authority %d times about a network-origin query; want none", n)
	}
}

// seeNodeStateDir resolves every name to one identity.
//
// note: the embedded nil interface panics on any other method.
type seeNodeStateDir struct {
	dirmod.Module
	id *astral.Identity
}

func (d *seeNodeStateDir) ResolveIdentity(string) (*astral.Identity, error) {
	return d.id, nil
}

// TestSeeNodeStateAnswersHolder is the allowed path: a caller holding the action
// receives the agent's record.
func TestSeeNodeStateAnswersHolder(t *testing.T) {
	agentID := astral.GenerateIdentity()
	db := testDB(t)

	row := &dbAgent{Identity: agentID, Alias: "scout", Token: "t", ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateAgent(row); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority, Dir: &seeNodeStateDir{id: agentID}}, db: db}
	w := newRecordingWriter()

	if err := route(t, mod.OpAgent, astral.GenerateIdentity(), "mcp.agent?id=scout", w); err != nil {
		t.Fatalf("mcp.agent refused a caller holding the action: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("mcp.agent did not finish for a caller holding the action")
	}

	if w.written() == 0 {
		t.Fatal("mcp.agent wrote nothing to a caller holding the action")
	}

	if n := len(authority.recorded()); n != 1 {
		t.Fatalf("mcp.agent made %d authorization calls; want exactly 1", n)
	}
}
