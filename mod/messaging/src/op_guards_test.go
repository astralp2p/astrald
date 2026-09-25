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

// mailOp is one mail operation and a query that binds its required arguments.
type mailOp struct {
	name string
	op   func(*Module) any
	args string
}

func mailOps() []mailOp {
	ref := "?box=inbox&id=" + messaging.NewMessageID().String()
	return []mailOp{
		{"messaging.send_message", func(m *Module) any { return m.OpSendMessage }, ""},
		{"messaging.list_messages", func(m *Module) any { return m.OpListMessages }, ""},
		{"messaging.read_messages", func(m *Module) any { return m.OpReadMessages }, ""},
		{"messaging.wait", func(m *Module) any { return m.OpWait }, "?timeout=10ms"},
		{"messaging.archive", func(m *Module) any { return m.OpArchive }, ref},
	}
}

// Every mail operation refuses a query off a link or from an MCP agent, and a
// caller whose mailbox this node does not host — an unhosted identity, the zero
// identity and the node's own — before it accepts or writes anything, and
// without asking the authority anything.
func TestAMailOperationServesOnlyALocalParticipant(t *testing.T) {
	for _, op := range mailOps() {
		mod := testMessagingModule(t)
		participant := hostedParticipant(t, mod)
		authority := mod.Auth.(*fakeAuth)

		for _, c := range []struct {
			name   string
			caller *astral.Identity
			origin string
		}{
			{"network origin", participant, astral.OriginNetwork},
			{"mcp origin", participant, astral.OriginMCP},
			{"unhosted caller", astral.GenerateIdentity(), astral.OriginLocal},
			{"zero caller", &astral.Identity{}, astral.OriginLocal},
			{"node caller", mod.node.Identity(), astral.OriginLocal},
		} {
			t.Run(op.name+"/"+c.name, func(t *testing.T) {
				w := newRecordingWriter()
				before := len(authority.questions())

				err := routeQuery(t, op.op(mod), originQuery(c.caller, op.name+op.args, c.origin), w)

				var rejected *astral.ErrRejected
				if !errors.As(err, &rejected) {
					t.Fatalf("%s answered: got err %v, want a rejection", op.name, err)
				}
				if n := w.written(); n != 0 {
					t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
				}
				if asked := authority.questions()[before:]; len(asked) != 0 {
					t.Fatalf("%s asked the authority %v about a refused caller; want nothing", op.name, asked)
				}
			})
		}
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

// callOp routes one query to one op the way a caller does: once the op
// accepts, it sends body when there is one, and it answers everything the op
// wrote before closing.
func callOp(t *testing.T, fn any, q *astral.InFlightQuery, body astral.Object) []astral.Object {
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
		t.Fatalf("route %v: %v", q.QueryString, err)
	}
	defer conn.Close()

	if body != nil {
		if err = channel.NewSender(conn).Send(body); err != nil {
			t.Fatalf("send the request: %v", err)
		}
	}
	return w.objects(t)
}
