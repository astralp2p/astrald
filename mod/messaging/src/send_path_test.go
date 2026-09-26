package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// localOrigins are the origins a query routed on this node carries: none, an
// explicit local one, and an agent's over MCP.
var localOrigins = []string{"", astral.OriginLocal, astral.OriginMCP}

// unmarkedQuery is a query from caller to target on path, stamped with origin
// the way the local entry points stamp one, and without the send path's mark.
func unmarkedQuery(caller, target *astral.Identity, path, origin string) *astral.InFlightQuery {
	q := astral.Launch(astral.NewQuery(caller, target, path))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}
	return q
}

// routeAndWrite routes q through the module. When the route is accepted, it
// writes obj and waits for the answer, so whatever the answering side stores is
// stored when it returns. It answers the routing error.
func routeAndWrite(t *testing.T, mod *Module, q *astral.InFlightQuery, obj astral.Object) error {
	t.Helper()

	w := &bufWriteCloser{}
	wc, err := mod.RouteQuery(mod.ctx, q, w)
	if err != nil {
		return err
	}

	if err = channel.NewSender(wc).Send(obj); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for w.String() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// A delivery that did not come over a link is taken only from this module's
// send path. The recipient's side asks receive_action alone and relies on
// send_action having been asked where the message was sent; a delivery an
// agent's declared tool put, or a local app routed itself, was never asked it.
// Such a delivery is rejected whether its target is hosted here or not, and
// stores nothing.
func TestALocalDeliveryIsTakenOnlyFromTheSendPath(t *testing.T) {
	mod := testMessagingModule(t)
	authority := authorityOf(mod)
	mod.Auth = authority
	authority.Add(authmod.Func[*messaging.ReceiveAction](func(*astral.Context, *messaging.ReceiveAction) bool { return true }))
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	// the control: send_action refuses a, so the send path puts nothing to b
	req := &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "sent"}
	if _, err := mod.SendMessage(context.Background(), a, req); err == nil || !strings.Contains(err.Error(), "unknown recipient") {
		t.Fatalf("a send send_action refuses: got %v, want the recipient refused", err)
	}

	for _, origin := range localOrigins {
		for name, target := range map[string]*astral.Identity{"hosted": b, "elsewhere": astral.GenerateIdentity()} {
			msg := &messaging.Message{ID: messaging.NewMessageID(), Content: "routed"}
			err := routeAndWrite(t, mod, unmarkedQuery(a, target, messaging.MethodMessage, origin), msg)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("a delivery with origin %q to a target %v: got %v, want a rejection", origin, name, err)
			}
		}
	}

	if n := countOwned(t, mod.db, b); n != 0 {
		t.Fatalf("b holds %v messages, want none: mail send_action refuses was stored", n)
	}
}

// A receipt that did not come over a link is taken only from this module's own
// read path. A recipient that routed one itself would stamp the sender's row
// collected without the body having been handed out.
func TestALocalReceiptIsTakenOnlyFromTheSendPath(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	for _, origin := range localOrigins {
		err := routeAndWrite(t, mod, unmarkedQuery(b, a, messaging.MethodReceipt, origin), &messaging.Receipt{ID: id})

		var rejected *astral.ErrRejected
		if !errors.As(err, &rejected) {
			t.Fatalf("a receipt with origin %q: got %v, want a rejection", origin, err)
		}
	}

	if sent := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxOutbox, ID: id}); sent.FetchedAt != nil {
		t.Fatal("a receipt from outside the send path stamped the sender's row fetched")
	}
}

// The mark is what launchDelivery sets and nothing else: a value of another
// type under the same key is not it.
func TestOnlyTheSendPathsOwnMarkCounts(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	if !fromSendPath(launchDelivery(a, b, messaging.MethodMessage)) {
		t.Fatal("launchDelivery's own query is not taken as the send path's")
	}

	forged := unmarkedQuery(a, b, messaging.MethodMessage, "")
	forged.Extra.Set(extraSendPath, true)
	if fromSendPath(forged) {
		t.Fatal("a value set by another package under the mark's key was taken as the mark")
	}
}
