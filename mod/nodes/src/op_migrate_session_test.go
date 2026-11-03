package nodes

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// migrateSessionQuery names sess and the fixture's target link, without start.
func migrateSessionQuery(f *migrationFixture, sess *session) string {
	return "nodes.migrate_session?session_id=" + sess.Nonce.String() + "&link_id=" + f.targetLink.id.String()
}

// linkPeerSessions returns the open sessions on the fixture's old link, by kind.
// The fixture's own session names the link peer as its logical peer. The other
// two name an app: one relayed in for an app on the peer, recorded as at
// Mux.handleRelayQuery, and one routed out to an app target, recorded as at
// Mux.RouteQuery for a local caller.
func linkPeerSessions(t *testing.T, f *migrationFixture, app *astral.Identity) map[string]*session {
	t.Helper()
	peer := f.oldLink.mux.RemoteIdentity()
	sessions := map[string]*session{"peer": f.session}
	for name, s := range map[string]struct {
		source   *astral.Identity
		outbound bool
	}{
		"relayed app caller": {peer, false},
		"app target":         {nil, true},
	} {
		sess, ok := f.oldLink.mux.createSession(astral.NewNonce(), app, s.source, "svc", s.outbound, defaultBufferSize)
		if !ok {
			t.Fatalf("create %v session", name)
		}
		sess.setState(stateOpen)
		sessions[name] = sess
	}
	return sessions
}

// TestMigrateSessionRefusesAResponderOtherThanThePeer: without start the op runs
// the responder side for the initiator, the node across the session's link.
// Another caller holding AdminNetwork is refused, including the session's logical
// peer when that is an app, and the session stays open on its link.
func TestMigrateSessionRefusesAResponderOtherThanThePeer(t *testing.T) {
	f := newMigrationFixture(t)
	f.mod.Deps.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()

	for name, sess := range linkPeerSessions(t, f, app) {
		for caller, id := range map[string]*astral.Identity{"another node": astral.GenerateIdentity(), "the app": app} {
			w := newRecordingWriter()
			if err := route(t, f.mod.OpMigrateSession, id, migrateSessionQuery(f, sess), w); err != nil {
				t.Fatalf("%v session, %v: route: %v", name, caller, err)
			}
			if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
				t.Fatalf("%v session, %v: answered %q; want %q", name, caller, got, want)
			}
			if _, moved := f.targetLink.mux.sessions.Get(sess.Nonce); !sess.IsOpen() || moved {
				t.Fatalf("%v session, %v: a caller other than the link peer moved the session", name, caller)
			}
		}
	}
}

// TestMigrateSessionAdmitsThePeerAsResponder: the node across the session's link
// passes the ownership check for every kind of session and reaches the signal
// exchange. Its channel closes before the ready signal, so the op answers the
// unexpected EOF instead of a refusal.
func TestMigrateSessionAdmitsThePeerAsResponder(t *testing.T) {
	f := newMigrationFixture(t)
	f.mod.Deps.Auth = &recordingAuth{verdict: true}
	peer := f.oldLink.mux.RemoteIdentity()

	for name, sess := range linkPeerSessions(t, f, astral.GenerateIdentity()) {
		w := newRecordingWriter()
		op, err := routing.NewOp(f.mod.OpMigrateSession)
		if err != nil {
			t.Fatalf("new op: %v", err)
		}
		// why: closing the caller's side also closes the answer writer, so the op
		// has answered only once it returns.
		returned := make(chan struct{})
		op.LogFunc = func(*routing.Report) { close(returned) }

		ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
		toOp, err := op.RouteQuery(ctx, astral.Launch(query.New(peer, peer, migrateSessionQuery(f, sess), nil)), w)
		if err != nil {
			cancel()
			t.Fatalf("%v session: route: %v", name, err)
		}
		toOp.Close()

		select {
		case <-returned:
		case <-time.After(10 * time.Second):
			t.Fatalf("%v session: the op never returned", name)
		}
		cancel()

		if got, want := w.refusal(t), io.ErrUnexpectedEOF.Error(); got != want {
			t.Fatalf("%v session: answered the link peer %q; want %q", name, got, want)
		}
	}
}
