package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

func TestClassify(t *testing.T) {
	expired, cancel := astral.NewContext(nil).WithTimeout(time.Nanosecond)
	defer cancel()
	<-expired.Done()
	cases := []struct {
		name    string
		err     error
		outcome string
		code    int
	}{
		{"accepted", nil, outcomeOK, 0},
		{"unavailable", &astral.ErrRejected{Code: 7}, outcomeUnavailable, 7},
		{"relayed timeout", &astral.ErrRejected{Code: 6}, outcomeTimeout, 6},
		{"refusal", &astral.ErrRejected{Code: 1}, "rejected:1", 1},
		{"op code", &astral.ErrRejected{Code: 9}, "rejected:9", 9},
		{"wrapped rejection", fmt.Errorf("hello: %w", &astral.ErrRejected{Code: 7}), outcomeUnavailable, 7},
		{"node route not found", &apphostapi.ErrorMsg{Code: apphostapi.ErrCodeRouteNotFound}, outcomeRouteNotFound, 0},
		{"node timeout", &apphostapi.ErrorMsg{Code: apphostapi.ErrCodeTimeout}, outcomeTimeout, 0},
		{"node canceled", &apphostapi.ErrorMsg{Code: apphostapi.ErrCodeCanceled}, outcomeCanceled, 0},
		{"target not allowed", &apphostapi.ErrorMsg{Code: apphostapi.ErrCodeTargetNotAllowed}, outcomeTargetNotAllowed, 0},
		{"denied", &apphostapi.ErrorMsg{Code: apphostapi.ErrCodeDenied}, outcomeError, 0},
		{"route not found", astral.NewErrRouteNotFound(), outcomeRouteNotFound, 0},
		{"query timeout", astral.ErrTimeout, outcomeTimeout, 0},
		{"deadline", context.DeadlineExceeded, outcomeTimeout, 0},
		{"read cut by the deadline", withCause(expired, io.ErrUnexpectedEOF), outcomeTimeout, 0},
		{"canceled", context.Canceled, outcomeCanceled, 0},
		{"other", errors.New("broken pipe"), outcomeError, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			outcome, code := classify(c.err)
			if outcome != c.outcome || code != c.code {
				t.Errorf("classify(%v) = %v, %v; want %v, %v", c.err, outcome, code, c.outcome, c.code)
			}
		})
	}
}

func TestWithCauseKeepsALiveContextsError(t *testing.T) {
	ctx := astral.NewContext(nil)
	if err := withCause(ctx, io.EOF); err != io.EOF {
		t.Errorf("withCause on a live context = %v; want io.EOF unchanged", err)
	}
	if err := withCause(ctx, nil); err != nil {
		t.Errorf("withCause(nil) = %v; want nil", err)
	}
}

// answer is a channel that holds objects sent to it, to be received back.
func answer(t *testing.T, objects ...astral.Object) *channel.Channel {
	t.Helper()
	buf := &bytes.Buffer{}
	ch := channel.New(buf)
	for _, o := range objects {
		if err := ch.Send(o); err != nil {
			t.Fatal(err)
		}
	}
	return ch
}

func TestReceiveOne(t *testing.T) {
	ctx := astral.NewContext(nil)
	id, err := astral.ParseIdentity("0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := receiveOne(ctx, answer(t, astral.NewString8("hello, x"), &astral.EOS{})); err != nil || got != "hello, x" {
		t.Errorf("a string answer = %q, %v; want hello, x", got, err)
	}
	if got, err := receiveOne(ctx, answer(t, id)); err != nil || got != id.String() {
		t.Errorf("an identity answer = %q, %v; want %v", got, err, id)
	}
	if _, err := receiveOne(ctx, answer(t, astral.NewError("boom"))); err == nil || err.Error() != "boom" {
		t.Errorf("an error answer = %v; want boom", err)
	}
	if _, err := receiveOne(ctx, answer(t)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("an empty answer = %v; want io.ErrUnexpectedEOF", err)
	}
}

func TestNewCallRecord(t *testing.T) {
	start := time.Unix(100, 0)
	c := newCallRecord(start, start.Add(1500*time.Millisecond), "", &astral.ErrRejected{Code: 7})
	if c.Start != 100 || c.End != 101.5 || c.Ms != 1500 {
		t.Errorf("times %v..%v, %v ms; want 100..101.5, 1500 ms", c.Start, c.End, c.Ms)
	}
	if c.Outcome != outcomeUnavailable || c.Code != 7 || c.Err != "query rejected (7)" {
		t.Errorf("outcome %v code %v err %q; want unavailable, 7, query rejected (7)", c.Outcome, c.Code, c.Err)
	}
	if ok := newCallRecord(start, start, "hello, x", nil); ok.Outcome != outcomeOK || ok.Err != "" || ok.Reply != "hello, x" {
		t.Errorf("an answered call = %+v; want ok with its reply", ok)
	}
}
