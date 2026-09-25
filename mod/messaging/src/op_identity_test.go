package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// messaging.create_identity mints a participant whose mailbox this node hosts
// from the moment it answers: a hosting contract the identity signed is
// indexed, and a message sent to its alias lands in the inbox it lists — on a
// store that holds no mcp table at all.
func TestTheCreateIdentityOpMintsAServedMailbox(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	peer := hostedParticipant(t, mod)
	before := time.Now()

	q := localQuery(astral.GenerateIdentity(), messaging.MethodCreateIdentity+"?alias=scout&duration=1h")
	cred := onlyAnswer[*messaging.IdentityCredential](t, collectOp(t, mod.OpCreateIdentity, q))

	if mcp, _ := hasTable(mod.db.DB, legacyAgents); mcp {
		t.Fatal("the store holds an mcp agent table; the mailbox must be served without one")
	}
	if n := len(hostingContracts(t, mod, cred.Identity)); n != 1 {
		t.Fatalf("%v hosting contracts indexed for the new identity, want 1", n)
	}
	if expires := cred.ExpiresAt.Time(); expires.Before(before.Add(time.Hour-time.Second)) || expires.After(time.Now().Add(time.Hour)) {
		t.Fatalf("the token expires at %v, want the hour the op was asked for", expires)
	}

	sent := sendOver(t, mod, localQuery(peer, messaging.MethodSendMessage), &messaging.SendMessageRequest{
		To: "scout", Content: "welcome",
	})

	inbox := listOver(t, mod, localQuery(cred.Identity, messaging.MethodListMessages))
	if len(inbox) != 1 || inbox[0].ID != sent || !inbox[0].Sender.IsEqual(peer) {
		t.Fatalf("the new participant's inbox holds %v messages, want the one sent to its alias", len(inbox))
	}
}

// messaging.delete_identity takes everything a participant held on this node:
// every token of its identity — the one issued and one reissued — its grants,
// its alias and the mail it owns in both boxes, archived or not. The
// correspondent keeps its own copies. Hosting is withdrawn here and the
// hosting contract is not revoked.
func TestTheDeleteIdentityOpWithdrawsTheParticipant(t *testing.T) {
	mod, apphost, dir := testIdentityModule(t)
	u, peer := hostedParticipant(t, mod), hostedParticipant(t, mod)
	dir.aliases["scout"] = u
	apphost.tokens["issued"], apphost.tokens["reissued"], apphost.tokens["peer's"] = u, u, peer
	apphost.grants[u.String()] = []*auth.Permit{{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}}
	correspond(t, mod, u, peer)
	peerHolds := countOwned(t, mod.db, peer)

	q := localQuery(astral.GenerateIdentity(), messaging.MethodDeleteIdentity+"?identity=scout")
	onlyAnswer[*astral.Ack](t, collectOp(t, mod.OpDeleteIdentity, q))

	if len(apphost.tokens) != 1 || !apphost.tokens["peer's"].IsEqual(peer) {
		t.Fatalf("tokens left %v, want only the correspondent's", apphost.tokens)
	}
	if n := len(apphost.grants[u.String()]); n != 0 {
		t.Fatalf("the deleted identity still holds %v grants", n)
	}
	if _, ok := dir.aliases["scout"]; ok {
		t.Fatal("the alias still names the deleted identity")
	}
	if n := countOwned(t, mod.db, u); n != 0 {
		t.Fatalf("the deleted identity still owns %v messages", n)
	}
	if n := countOwned(t, mod.db, peer); n != peerHolds {
		t.Fatalf("the correspondent holds %v messages after the delete, want its %v", n, peerHolds)
	}
	checkWithdrawn(t, mod, u)
}

// correspond exchanges mail so that u owns a row in each box and an archived
// one, and peer owns its own copy of each message.
func correspond(t *testing.T, mod *Module, u, peer *astral.Identity) {
	t.Helper()

	send := func(from, to *astral.Identity, parent messaging.MessageID) messaging.MessageID {
		id, err := mod.SendMessage(context.Background(), from, &messaging.SendMessageRequest{
			To: astral.String8(to.String()), Content: "x", ParentID: parent,
		})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		return id
	}

	ask := send(peer, u, messaging.MessageID{})
	send(u, peer, ask)
	later := send(peer, u, messaging.MessageID{})

	if _, err := mod.Archive(context.Background(), u, messaging.MessageRef{Box: messaging.BoxInbox, ID: later}, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if n := countOwned(t, mod.db, u); n != 3 {
		t.Fatalf("u owns %v messages before the delete, want 3", n)
	}
}

// checkWithdrawn asserts this node no longer hosts u's mailbox — no index row,
// no delivery routed to it — while the hosting contract u signed stays
// indexed: the deletion withdrew hosting here and revoked nothing.
func checkWithdrawn(t *testing.T, mod *Module, u *astral.Identity) {
	t.Helper()

	if _, err := mod.db.FindMailbox(u); err == nil {
		t.Fatal("the index row outlived the delete")
	}
	if mod.hosts(u) {
		t.Fatal("the node still hosts the deleted identity's mailbox")
	}
	if _, err := mod.RouteQuery(mod.ctx, inFlight(u, messaging.MethodMessage), &bufWriteCloser{}); !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("a delivery to the deleted identity: got %v, want route not found", err)
	}
	if n := len(hostingContracts(t, mod, u)); n != 1 {
		t.Fatalf("%v hosting contracts indexed after the delete, want the one provisioned — deletion revokes none", n)
	}
}

// messaging.identity and messaging.delete_identity answer what they cannot
// find in the words those operations always used: a name nothing resolves is an
// unknown identity, and an identity whose mailbox the index does not name is
// not found.
func TestTheIdentityOpsNameWhatTheyCannotFind(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	stranger := astral.GenerateIdentity()

	for _, op := range []struct {
		name string
		fn   any
	}{
		{messaging.MethodIdentity, mod.OpIdentity},
		{messaging.MethodDeleteIdentity, mod.OpDeleteIdentity},
	} {
		for arg, want := range map[string]string{"nobody": "unknown identity", stranger.String(): "identity not found"} {
			q := localQuery(astral.GenerateIdentity(), op.name+"?identity="+arg)
			objs := collectOp(t, op.fn, q)
			if len(objs) != 1 {
				t.Fatalf("%v?identity=%v answered %v objects, want one error", op.name, arg, len(objs))
			}

			if e, ok := objs[0].(astral.Error); !ok || e.Error() != want {
				t.Fatalf("%v?identity=%v answered %v, want the error %q", op.name, arg, objs[0], want)
			}
		}
	}
}
