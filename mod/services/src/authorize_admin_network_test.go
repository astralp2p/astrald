package services

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
)

// adminNetworkDir resolves no name. services.sync resolves its target right
// after the check, so the allowed-path test needs a directory that answers;
// every other method panics.
type adminNetworkDir struct {
	dir.Module
}

func (adminNetworkDir) ResolveIdentity(string) (*astral.Identity, error) {
	return nil, errors.New("no such identity")
}

// TestAdminNetworkRefusesCallerWithoutPermits is the coverage measure for the
// AdminNetwork action in mod/services: services.sync asks before it acts and
// rejects when the answer is no.
//
// note: the module is a bare struct. An op that reached past its check would
// accept the query and panic on the nil directory.
func TestAdminNetworkRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: false}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	err := route(t, mod.OpSync, caller, "services.sync?id=anything", w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("answered a caller holding no permits: got err %v, want a rejection", err)
	}

	if n := w.written(); n != 0 {
		t.Fatalf("wrote %d bytes to a refused caller; want none", n)
	}

	requireAdminNetworkAsked(t, authority, caller)
}

// TestAdminNetworkAdmitsAuthorizedCaller shows the check passes a granted
// caller through: services.sync accepts and answers the unresolvable target
// with an error.
func TestAdminNetworkAdmitsAuthorizedCaller(t *testing.T) {
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority, Dir: adminNetworkDir{}}}
	w := newRecordingWriter()

	err := route(t, mod.OpSync, caller, "services.sync?id=anything", w)
	if err != nil {
		t.Fatalf("refused an authorized caller: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}

	if w.written() == 0 {
		t.Fatal("wrote nothing to an authorized caller")
	}

	requireAdminNetworkAsked(t, authority, caller)
}

// requireAdminNetworkAsked asserts exactly one AdminNetwork question, naming the
// caller as the actor.
func requireAdminNetworkAsked(t *testing.T, authority *recordingAuth, caller *astral.Identity) {
	t.Helper()

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("made %d authorization calls; want exactly 1", len(actions))
	}

	action, ok := actions[0].(*auth.AdminNetworkAction)
	if !ok {
		t.Fatalf("named %q; want %q", actions[0].ObjectType(), (&auth.AdminNetworkAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("named actor %v; want the caller %v", action.Actor(), caller)
	}
}
