package messaging

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// mailOp is one mail operation, a query that binds its required arguments, and
// the request it reads once accepted, when it reads one.
//
// answersUnhosted says the operation accepts a caller whose mailbox this node
// does not host, reads its request, and answers errNotParticipant:
// read_messages learns the mailbox it reads from its request, and a delegated
// reader need not be hosted.
type mailOp struct {
	name            string
	op              func(*Module) any
	args            string
	body            astral.Object
	answersUnhosted bool
}

func mailOps() []mailOp {
	id := messaging.NewMessageID()
	ref := "?box=inbox&id=" + id.String()
	return []mailOp{
		{name: "messaging.send_message", op: func(m *Module) any { return m.OpSendMessage },
			body: &messaging.SendMessageRequest{To: "anyone", Content: "x"}},
		{name: "messaging.list_messages", op: func(m *Module) any { return m.OpListMessages }},
		{name: "messaging.read_messages", op: func(m *Module) any { return m.OpReadMessages },
			body:            &messaging.ReadMessagesRequest{Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: id}}},
			answersUnhosted: true},
		{name: "messaging.wait", op: func(m *Module) any { return m.OpWait }, args: "?timeout=10ms"},
		{name: "messaging.archive", op: func(m *Module) any { return m.OpArchive }, args: ref},
	}
}

// Every mail operation refuses a query off a link or from an MCP agent, and a
// caller whose mailbox this node does not host — an unhosted identity, the zero
// identity and the node's own — without asking the authority anything. Each
// rejects the query before it accepts it, except read_messages for an unhosted
// caller: it accepts, reads the request, and answers that the caller is not a
// messaging participant.
func TestAMailOperationServesOnlyALocalParticipant(t *testing.T) {
	for _, op := range mailOps() {
		mod := testMessagingModule(t)
		participant := hostedParticipant(t, mod)
		authority := mod.Auth.(*fakeAuth)
		unhosted := astral.GenerateIdentity()

		for _, c := range []struct {
			name   string
			caller *astral.Identity
			origin string
		}{
			{"network origin", participant, astral.OriginNetwork},
			{"mcp origin", participant, astral.OriginMCP},
			{"unhosted caller", unhosted, astral.OriginLocal},
			{"zero caller", &astral.Identity{}, astral.OriginLocal},
			{"node caller", mod.node.Identity(), astral.OriginLocal},
		} {
			t.Run(op.name+"/"+c.name, func(t *testing.T) {
				before := len(authority.questions())

				res := tryOp(t, op.op(mod), originQuery(c.caller, op.name+op.args, c.origin), op.body)

				checkUnhosted(t, op, res, c.caller == unhosted)
				if asked := authority.questions()[before:]; len(asked) != 0 {
					t.Fatalf("%s asked the authority %v about a refused caller; want nothing", op.name, asked)
				}
			})
		}
	}
}

// checkUnhosted asserts how op refused a caller: with errNotParticipant where
// the op answers an unhosted caller and the caller is one, and with nothing
// otherwise.
func checkUnhosted(t *testing.T, op mailOp, res opResult, unhosted bool) {
	t.Helper()

	if op.answersUnhosted && unhosted {
		checkNotParticipant(t, op.name, res)
		return
	}
	checkRefused(t, op.name, res, false)
}

// checkNotParticipant asserts the op accepted the query and answered one error
// object, errNotParticipant's words, and nothing else.
func checkNotParticipant(t *testing.T, name string, res opResult) {
	t.Helper()

	if res.err != nil {
		t.Fatalf("%s: got err %v, want the query accepted and answered", name, res.err)
	}
	if len(res.objs) != 1 {
		t.Fatalf("%s answered %v objects; want one error", name, len(res.objs))
	}
	said, ok := res.objs[0].(*astral.ErrorMessage)
	if !ok || said.Error() != errNotParticipant.Error() {
		t.Fatalf("%s answered %#v; want the error %q", name, res.objs[0], errNotParticipant)
	}
}

// checkRefused asserts the caller was answered nothing: the query rejected, or,
// where afterRequest says the op refuses only once its request arrives,
// accepted and ended with no answer.
func checkRefused(t *testing.T, name string, res opResult, afterRequest bool) {
	t.Helper()

	var rejected *astral.ErrRejected
	switch {
	case afterRequest && res.err != nil:
		t.Fatalf("%s: got err %v, want the query accepted and ended with no answer", name, res.err)
	case !afterRequest && !errors.As(res.err, &rejected):
		t.Fatalf("%s answered: got err %v, want a rejection", name, res.err)
	case len(res.objs) != 0:
		t.Fatalf("%s answered a refused caller %v objects; want none", name, len(res.objs))
	}
}

// A mail operation acts on the caller's own boxes, and no argument names
// another: an owner a caller adds is not one the operation reads.
func TestAMailOperationTakesNoOwner(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "a's"})

	for _, arg := range []string{"owner", "identity", "caller", "recipient"} {
		objs := collectOp(t, mod.OpListMessages, originQuery(b, "messaging.list_messages?"+arg+"="+a.String(), astral.OriginLocal))

		for _, obj := range objs {
			if env, ok := obj.(*messaging.Envelope); ok && env.Recipient.IsEqual(a) {
				t.Fatalf("%v=%v listed a's inbox to b", arg, a)
			}
		}
		if len(objs) != 1 {
			t.Fatalf("%v: b's own empty inbox answered %v objects, want eos alone", arg, len(objs))
		}
	}
}

// A delivery and a receipt are addressed to a participant. Routed without an
// origin, one aimed at the node itself reaches this module's operations — so no
// operation may answer to either path.
func TestNoOperationAnswersADeliveryPath(t *testing.T) {
	mod := testMessagingModule(t)
	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		t.Fatalf("add ops: %v", err)
	}

	for _, method := range []string{messaging.MethodMessage, messaging.MethodReceipt} {
		name, scoped := strings.CutPrefix(method, mod.String()+".")
		if !scoped || mod.router.HasRoute(name) {
			t.Fatalf("an operation of this module answers %v", method)
		}
	}

	if !mod.router.HasRoute("send_message") {
		t.Fatal("the router holds none of the module's operations; the check above proves nothing")
	}
}

// collectingWriter is the caller's end of the connection, kept whole so the
// answer can be decoded once the op closes it.
type collectingWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	done   atomic.Bool
	closed chan struct{}
}

func newCollectingWriter() *collectingWriter {
	return &collectingWriter{closed: make(chan struct{})}
}

func (w *collectingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *collectingWriter) Close() error {
	if w.done.CompareAndSwap(false, true) {
		close(w.closed)
	}
	return nil
}

// objects decodes every object the op wrote, once it has closed the conn.
func (w *collectingWriter) objects(t *testing.T) []astral.Object {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var objs []astral.Object
	recv := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	for {
		obj, err := recv.Receive()
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		objs = append(objs, obj)
	}
}

// collectOp routes one query to one op, accepted, and answers what it wrote.
func collectOp(t *testing.T, fn any, q *astral.InFlightQuery) []astral.Object {
	t.Helper()
	return callOp(t, fn, q, nil)
}

// callOp routes one query to one op that accepts it, and answers everything the
// op wrote before closing — see tryOp.
func callOp(t *testing.T, fn any, q *astral.InFlightQuery, body astral.Object) []astral.Object {
	t.Helper()

	res := tryOp(t, fn, q, body)
	if res.err != nil {
		t.Fatalf("route %v: %v", q.QueryString, res.err)
	}
	return res.objs
}

// opResult is how an op resolved one query: the router's verdict, and every
// object the op wrote once it accepted.
type opResult struct {
	objs []astral.Object
	err  error
}

// tryOp routes one query to one op the way a caller does. Once the op accepts,
// tryOp sends body when there is one, and collects everything the op wrote
// before closing.
func tryOp(t *testing.T, fn any, q *astral.InFlightQuery, body astral.Object) opResult {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	w := newCollectingWriter()
	conn, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		return opResult{err: err}
	}
	defer conn.Close()

	if body != nil {
		if err = channel.NewSender(conn).Send(body); err != nil {
			t.Fatalf("send the request: %v", err)
		}
	}
	return opResult{objs: w.objects(t)}
}
