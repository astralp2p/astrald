package nodes

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// migrateSessionQuery names the fixture's session and target link, without start.
func migrateSessionQuery(f *migrationFixture) string {
	return "nodes.migrate_session?session_id=" + f.session.Nonce.String() + "&link_id=" + f.targetLink.id.String()
}

// TestMigrateSessionRefusesAResponderOtherThanThePeer: without start the op runs
// the responder side for the initiator at the session's far end. Another caller
// holding AdminNetwork is refused, and the session stays open on its link.
func TestMigrateSessionRefusesAResponderOtherThanThePeer(t *testing.T) {
	f := newMigrationFixture(t)
	f.mod.Deps.Auth = &recordingAuth{verdict: true}
	w := newRecordingWriter()

	err := route(t, f.mod.OpMigrateSession, astral.GenerateIdentity(), migrateSessionQuery(f), w)

	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if !f.session.IsOpen() || f.migrated() {
		t.Fatal("a caller other than the session's peer moved the session")
	}
}

// TestMigrateSessionAdmitsThePeerAsResponder: the session's peer passes the
// ownership check and reaches the signal exchange. Its channel closes before the
// ready signal, so the op answers that failure instead of a refusal.
func TestMigrateSessionAdmitsThePeerAsResponder(t *testing.T) {
	f := newMigrationFixture(t)
	f.mod.Deps.Auth = &recordingAuth{verdict: true}
	w := newRecordingWriter()

	op, err := routing.NewOp(f.mod.OpMigrateSession)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	peer := f.session.RemoteIdentity
	toOp, err := op.RouteQuery(ctx, astral.Launch(query.New(peer, peer, migrateSessionQuery(f), nil)), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	toOp.Close()

	if got := w.refusal(t); got == auth.ErrNotPermitted.Error() {
		t.Fatalf("refused the session's peer: %q", got)
	}
}
