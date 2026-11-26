package user

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// opNode is a node whose every query is answered by one router, as the node it
// queries would: the network between a digest client and a handler.
type opNode struct {
	astral.Node
	id *astral.Identity
	op astral.Router
}

func (n *opNode) Identity() *astral.Identity { return n.id }

func (n *opNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return n.op.RouteQuery(ctx, q, w)
}

type routerFunc func(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error)

func (f routerFunc) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return f(ctx, q, w)
}

// answerWith answers every query by running fn on the accepted channel.
func answerWith(fn func(ch *channel.Channel)) astral.Router {
	return routerFunc(func(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
		return query.Accept(q, w, func(conn astral.Conn) {
			ch := channel.New(conn)
			defer ch.Close()
			fn(ch)
		})
	})
}

// handlerOf is the router of f's OpRelayDigest.
func handlerOf(t *testing.T, f *receiverFixture) astral.Router {
	t.Helper()

	op, err := routing.NewOp(f.mod.OpRelayDigest)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	return op
}

// clientOn is a module that queries router as asker, with the real digest client.
func clientOn(asker *astral.Identity, router astral.Router) *Module {
	return &Module{node: &opNode{id: asker, op: router}, ctx: astral.NewContext(nil)}
}

// TestRelayDigestClientReadsTheHandlersDigest: the client, as this node, reads
// the ids a member's OpRelayDigest streams, through the real query path.
func TestRelayDigestClientReadsTheHandlersDigest(t *testing.T) {
	userID := astral.GenerateIdentity()
	receiver := newReceiverFixture(t, userID)
	asker := astral.GenerateIdentity()
	receiver.auth.seed(membershipContract(userID, asker))

	a, b := relayForContract(astral.GenerateIdentity(), asker), relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity())
	receiver.auth.seed(a)
	receiver.auth.seed(b)

	digest, err := clientOn(asker, handlerOf(t, receiver)).fetchRelayDigest(astral.NewContext(nil), receiver.nodeID)
	if err != nil {
		t.Fatalf("fetchRelayDigest: %v", err)
	}
	idA, idB := objectIDOf(t, a), objectIDOf(t, b)
	if len(digest) != 2 || !digest.has(&idA) || !digest.has(&idB) {
		t.Fatalf("digest %v; want the ids of both relay contracts", digest)
	}
}

// TestRelayDigestClientFailsWithoutAWholeDigest: every answer that is not a
// stream of ids ended by eos is an error and no digest, never an empty digest.
func TestRelayDigestClientFailsWithoutAWholeDigest(t *testing.T) {
	userID := astral.GenerateIdentity()
	receiver := newReceiverFixture(t, userID)
	id := objectIDOf(t, relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity()))

	stranger := astral.GenerateIdentity() // not a member: the real handler refuses it
	cases := map[string]struct {
		asker  *astral.Identity
		router astral.Router
		want   error
	}{
		"refusal": {stranger, handlerOf(t, receiver), auth.ErrNotPermitted},
		"error message": {astral.GenerateIdentity(), answerWith(func(ch *channel.Channel) {
			ch.Send(astral.NewError("boom"))
		}), astral.NewError("boom")},
		"stream without eos": {astral.GenerateIdentity(), answerWith(func(ch *channel.Channel) {
			ch.Send(&id)
		}), errDigestUnfinished},
		"no answer": {astral.GenerateIdentity(), answerWith(func(ch *channel.Channel) {}), errDigestUnfinished},
		"another object": {astral.GenerateIdentity(), answerWith(func(ch *channel.Channel) {
			s := astral.String8("x")
			ch.Send(&s)
		}), nil},
		"query rejected": {astral.GenerateIdentity(), routerFunc(func(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
			return query.Reject()
		}), nil},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			digest, err := clientOn(c.asker, c.router).fetchRelayDigest(astral.NewContext(nil), receiver.nodeID)
			if err == nil {
				t.Fatalf("fetchRelayDigest returned digest %v; want an error", digest)
			}
			if digest != nil {
				t.Fatalf("fetchRelayDigest returned digest %v with its error; want none", digest)
			}
			if c.want != nil && err.Error() != c.want.Error() {
				t.Fatalf("fetchRelayDigest failed with %q; want %q", err, c.want)
			}
		})
	}
}

// TestRelayDigestClientTimesOut: a sibling that never answers ends the query at
// the client's own deadline, with an error and no digest.
func TestRelayDigestClientTimesOut(t *testing.T) {
	// note: the sibling answers nothing until the client closes the query, as a stalled link does.
	mod := clientOn(astral.GenerateIdentity(), answerWith(func(ch *channel.Channel) { ch.Receive() }))
	mod.digestTimeout = 50 * time.Millisecond

	start := time.Now()
	digest, err := mod.fetchRelayDigest(astral.NewContext(nil), astral.GenerateIdentity())
	if !errors.Is(err, context.DeadlineExceeded) || digest != nil {
		t.Fatalf("fetchRelayDigest returned %v, %v; want the deadline error and no digest", digest, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetchRelayDigest took %v; want its own short deadline", elapsed)
	}
}
