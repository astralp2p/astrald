package messaging

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// TestSeeNodeStateKeepsOriginRefusal: a query off a link or from an MCP agent
// is refused by origin, as before the guard, and the authority is never asked.
func TestSeeNodeStateKeepsOriginRefusal(t *testing.T) {
	for _, origin := range []string{astral.OriginNetwork, astral.OriginMCP} {
		t.Run(origin, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			caller := astral.GenerateIdentity()
			q := originQuery(caller, "messaging.identity?identity="+caller.String(), origin)

			err := routeQuery(t, mod.OpIdentity, q, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("messaging.identity answered a %v-origin query: got err %v, want a rejection", origin, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("messaging.identity wrote %d bytes to a %v-origin query; want none", n, origin)
			}

			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("messaging.identity asked the authority %d times about a %v-origin query; want none", n, origin)
			}
		})
	}
}

// seeNodeStateDir resolves every name to one identity and knows its alias.
//
// note: the embedded nil interface panics on any other method.
type seeNodeStateDir struct {
	dirmod.Module
	id    *astral.Identity
	alias string
}

func (d *seeNodeStateDir) ResolveIdentity(string) (*astral.Identity, error) {
	return d.id, nil
}

func (d *seeNodeStateDir) GetAlias(*astral.Identity) (string, error) {
	return d.alias, nil
}

// TestSeeNodeStateAnswersHolder is the allowed path: a caller holding the action
// receives the participant's record.
func TestSeeNodeStateAnswersHolder(t *testing.T) {
	identity := astral.GenerateIdentity()
	db := testDB(t)

	if err := db.CreateMailbox(identity, &astral.ObjectID{Size: 1}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority, Dir: &seeNodeStateDir{id: identity, alias: "scout"}}, db: db}
	w := newRecordingWriter()

	if err := route(t, mod.OpIdentity, astral.GenerateIdentity(), "messaging.identity?identity=scout", w); err != nil {
		t.Fatalf("messaging.identity refused a caller holding the action: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("messaging.identity did not finish for a caller holding the action")
	}

	if w.written() == 0 {
		t.Fatal("messaging.identity wrote nothing to a caller holding the action")
	}

	if n := len(authority.recorded()); n != 1 {
		t.Fatalf("messaging.identity made %d authorization calls; want exactly 1", n)
	}
}
