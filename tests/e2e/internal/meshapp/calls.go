package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// The outcome classes of a call (astral-docs core-definitions/query.md,
// Routing outcomes).
const (
	outcomeOK               = "ok"
	outcomeRouteNotFound    = "route_not_found"
	outcomeUnavailable      = "unavailable"
	outcomeTimeout          = "timeout"
	outcomeCanceled         = "canceled"
	outcomeTargetNotAllowed = "target_not_allowed"
	outcomeError            = "error"
)

// opHello is the op every mesh app serves.
const opHello = "hello"

// classify maps a call's error to its outcome class and reject code.
// A rejection is rejected:<code>, except 7, unavailable, and 6, the timeout
// a relaying node sends across a link (core-definitions/query.md:54-62).
func classify(err error) (outcome string, code int) {
	var rejected *astral.ErrRejected
	var msg *apphostapi.ErrorMsg
	switch {
	case err == nil:
		return outcomeOK, 0
	case errors.As(err, &rejected):
		return rejectedOutcome(rejected.Code), int(rejected.Code)
	case errors.As(err, &msg):
		return errorMsgOutcome(string(msg.Code)), 0
	case errors.Is(err, astral.NewErrRouteNotFound()):
		return outcomeRouteNotFound, 0
	case errors.Is(err, astral.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout, 0
	case errors.Is(err, context.Canceled):
		return outcomeCanceled, 0
	}
	return outcomeError, 0
}

func rejectedOutcome(code uint8) string {
	switch code {
	case astral.CodeUnavailable:
		return outcomeUnavailable
	case astral.CodeTimeout:
		return outcomeTimeout
	}
	return fmt.Sprintf("rejected:%d", code)
}

// errorMsgOutcome maps the apphost error a caller's own node answers.
func errorMsgOutcome(code string) string {
	switch code {
	case apphostapi.ErrCodeRouteNotFound:
		return outcomeRouteNotFound
	case apphostapi.ErrCodeTimeout:
		return outcomeTimeout
	case apphostapi.ErrCodeCanceled:
		return outcomeCanceled
	case apphostapi.ErrCodeTargetNotAllowed:
		return outcomeTargetNotAllowed
	}
	return outcomeError
}

// newCallRecord is one finished call with its outcome.
func newCallRecord(start, end time.Time, reply string, err error) callRecord {
	c := callRecord{Start: unixSeconds(start), End: unixSeconds(end), Reply: reply,
		Ms: float64(end.Sub(start).Microseconds()) / 1000}
	c.Outcome, c.Code = classify(err)
	if err != nil {
		c.Err = err.Error()
	}
	return c
}

// ask queries target's op as caller within ctx and returns the first object
// of the answer, rendered.
func ask(ctx *astral.Context, caller *astrald.Client, target *astral.Identity, op string) (string, error) {
	ch, err := caller.WithTarget(target).QueryChannel(ctx, op, nil)
	if err != nil {
		return "", withCause(ctx, err)
	}
	defer ch.Close()
	reply, err := receiveOne(ctx, ch)
	return reply, withCause(ctx, err)
}

// receiveOne reads the first object of an answer; an error object is the
// error.
func receiveOne(ctx *astral.Context, ch *channel.Channel) (string, error) {
	var reply astral.Object
	err := ch.Switch(channel.PassErrors, func(o astral.Object) error {
		reply = o
		return channel.ErrBreak
	}, channel.WithContext(ctx), channel.FailOnEOF())
	if err != nil {
		return "", err
	}
	return render(reply), nil
}

func render(o astral.Object) string {
	switch v := o.(type) {
	case *astral.String8:
		return string(*v)
	case *astral.Identity:
		return v.String()
	}
	return fmt.Sprintf("%v: %v", o.ObjectType(), o)
}

// withCause adds the end of ctx to err.
// why: a channel the context closed fails with a read error that names no
// deadline, and the outcome class depends on it.
func withCause(ctx *astral.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	return fmt.Errorf("%w: %w", err, ctx.Err())
}

// mesh is a serving app's calling side.
type mesh struct {
	rec         *recorder
	log         *callLog
	client      *astrald.Client
	self        string
	service     string
	period      time.Duration
	keepRemoved bool
	probeOp     string
	callTimeout time.Duration
}

// spawnCaller starts the calls to a peer first seen.
func (m *mesh) spawnCaller(ctx *astral.Context, wg *sync.WaitGroup, kind string, target *astral.Identity) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.callLoop(ctx, kind, target)
	}()
}

// callLoop calls the peer at once, then once per period while it is
// callable; a zero period makes one call.
func (m *mesh) callLoop(ctx *astral.Context, kind string, target *astral.Identity) {
	id := target.String()
	for {
		next := time.Now().Add(m.period)
		if m.rec.callable(kind, id, m.keepRemoved) {
			m.callOnce(ctx, kind, target)
		}
		if m.period <= 0 || !sleepUntil(ctx, next) {
			return
		}
	}
}

// callOnce makes one call to the peer and records it.
func (m *mesh) callOnce(ctx *astral.Context, kind string, target *astral.Identity) {
	id := target.String()
	op, want := m.probeOp, ""
	if kind == kindApp {
		op, want = opHello, "hello, "+m.self
	}
	start := time.Now()
	m.rec.started(kind, id, unixSeconds(start))
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(m.callTimeout)
	reply, err := ask(cctx, m.client, target, op)
	cancel()
	end := time.Now()
	if ctx.Err() != nil {
		// why: the app is stopping, and its own cancel is no outcome of the peer
		return
	}
	if err == nil && want != "" && reply != want {
		err = fmt.Errorf("reply %q, want %q", reply, want)
	}
	c := newCallRecord(start, end, reply, err)
	m.rec.finished(kind, id, c)
	m.log.append(logEntry{T0: c.Start, T1: c.End, To: id, Kind: kind,
		Outcome: c.Outcome, Code: c.Code, Ms: c.Ms})
}

// sleepUntil waits until t; false when ctx ends first.
func sleepUntil(ctx *astral.Context, t time.Time) bool {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
