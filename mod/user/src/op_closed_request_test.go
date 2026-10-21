package user

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
)

// TestContractOpsAnswerARequesterClosedBeforeTheContract: user.accept_contract
// and user.accept_membership whose caller closes before sending the contract
// answer io.ErrUnexpectedEOF; neither op acts on a nil contract.
func TestContractOpsAnswerARequesterClosedBeforeTheContract(t *testing.T) {
	ops := []struct {
		name string
		op   func(*Module) any
	}{
		{"user.accept_contract", func(m *Module) any { return m.OpAcceptContract }},
		{"user.accept_membership", func(m *Module) any { return m.OpAcceptMembership }},
	}

	for _, tt := range ops {
		t.Run(tt.name, func(t *testing.T) {
			// why: the real auth module, because its contract verification is
			// what a nil contract reaches.
			mod := &Module{Deps: Deps{Auth: &authsrc.Module{}}}

			answer, report := routeClosedRequester(t, tt.op(mod), tt.name)
			if report.Err != nil {
				t.Fatalf("the op failed: %v", report.Err)
			}
			if len(answer) != 1 {
				t.Fatalf("the op answered %v objects, want one error", len(answer))
			}
			e, ok := answer[0].(astral.Error)
			if !ok {
				t.Fatalf("the op answered %T, want an error", answer[0])
			}
			if !strings.Contains(e.Error(), io.ErrUnexpectedEOF.Error()) {
				t.Fatalf("the op answered %q, want %q", e.Error(), io.ErrUnexpectedEOF)
			}
		})
	}
}

// routeClosedRequester routes one query to one op, closes the requester's
// side once the op accepts, and answers what the op wrote and its report
// once the op has returned.
func routeClosedRequester(t *testing.T, fn any, queryString string) ([]astral.Object, *routing.Report) {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	caller := astral.GenerateIdentity()
	w := &answerWriter{}
	requester, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	requester.Close()

	var report *routing.Report
	select {
	case report = <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}
	return w.objects(t), report
}

// answerWriter is the caller's end of the connection; it keeps every byte the
// op wrote.
type answerWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *answerWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *answerWriter) Close() error { return nil }

func (w *answerWriter) objects(t *testing.T) []astral.Object {
	t.Helper()
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
