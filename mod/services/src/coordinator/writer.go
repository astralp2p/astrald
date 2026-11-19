package coordinator

import (
	"sort"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

type outKind int

const (
	outNone outKind = iota
	outRemoved
	outNotGranted
	outBoundary
	outView
)

type output struct {
	kind       outKind
	key        offeringKey
	view       *services.Update
	tagged     bool // write view as a services.tagged_update
	open       bool // with tagged: the provider's visibility when the view is written
	removed    []offeringKey
	cause      services.RemovalCause // with removed: the cause of every key
	incomplete *services.Incomplete
	notGranted *services.NotGranted
}

// Run writes the stream to sink until the stream closes, a one-shot sends its
// boundary, or a write fails. It is the stream's only writer.
//
// why: the writer waits only when it has nothing to send, so a wake that
// arrives while it is busy is redundant and never lost.
func (st *Stream) Run(sink Sink) error {
	c := st.c
	for {
		c.mu.Lock()
		if st.closed {
			c.mu.Unlock()
			return nil
		}
		out := st.nextOutput()
		c.mu.Unlock()

		if out.kind == outNone {
			<-st.wake
			continue
		}

		err := out.write(sink)

		c.mu.Lock()
		if err != nil {
			st.closeLocked()
			c.mu.Unlock()
			return err
		}
		done := st.markWritten(out)
		c.mu.Unlock()
		if done {
			return nil
		}
	}
}

// nextOutput picks what to send: removals of the lowest pending cause, then a
// changed not_granted list, then the boundary once nothing it waits for is
// left, then views in first-unsent order.
// why: removals and the new list precede views, so a consumer never reads a
// view the change it was told about would withhold.
func (st *Stream) nextOutput() output {
	if len(st.unsentRemovals) > 0 {
		return st.nextRemoved()
	}
	if ng := st.unsentNotGranted; ng != nil {
		st.unsentNotGranted = nil
		return output{kind: outNotGranted, notGranted: ng}
	}
	if at := st.attempt; at != nil && at.resolved && len(at.unsentAtBoundary) == 0 {
		return output{kind: outBoundary, incomplete: at.incomplete()}
	}
	if len(st.sendOrder) > 0 {
		k := st.sendOrder[0]
		st.sendOrder = st.sendOrder[1:]
		v := st.unsent[k]
		delete(st.unsent, k)
		st.inflight = &k
		st.inflightAvailable = bool(v.Available)
		out := output{kind: outView, key: k, view: v, tagged: st.tagged}
		if st.tagged {
			// why: the tag is read when the view is written, so a view
			// re-evaluated after a private list change carries the new value.
			out.open = st.c.isOpenKey(k)
		}
		return out
	}
	return output{}
}

// nextRemoved collects the pending removals of the lowest cause, sorted by
// key. One services.removed carries one cause.
func (st *Stream) nextRemoved() output {
	var cause services.RemovalCause
	for _, c := range st.unsentRemovals {
		if cause == 0 || c < cause {
			cause = c
		}
	}
	var keys []offeringKey
	for k, c := range st.unsentRemovals {
		if c == cause {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].provider+"\x00"+keys[i].name < keys[j].provider+"\x00"+keys[j].name
	})
	return output{kind: outRemoved, removed: keys, cause: cause}
}

// write sends one output to the sink.
func (out output) write(sink Sink) error {
	switch out.kind {
	case outRemoved:
		r := &services.Removed{Cause: astral.Uint8(out.cause)}
		for _, k := range out.removed {
			id, err := astral.ParseIdentity(k.provider)
			if err != nil {
				return err
			}
			r.Offerings = append(r.Offerings, &services.OfferingKey{ProviderID: id, Name: astral.String8(k.name)})
		}
		return sink.Removed(r)
	case outNotGranted:
		return sink.NotGranted(out.notGranted)
	case outBoundary:
		return sink.Boundary(out.incomplete)
	default:
		if out.tagged {
			return sink.TaggedUpdate(&services.TaggedUpdate{Open: astral.Bool(out.open), Update: out.view})
		}
		return sink.Update(out.view)
	}
}

// markWritten records a successful write and reports whether the stream is
// done.
func (st *Stream) markWritten(out output) bool {
	switch out.kind {
	case outRemoved:
		for _, k := range out.removed {
			delete(st.unsentRemovals, k)
			delete(st.visible, k)
			delete(st.written, k)
		}
	case outView:
		if bool(out.view.Available) {
			st.visible[out.key] = struct{}{}
		} else {
			delete(st.visible, out.key)
		}
		st.recordTagged(out)
		if st.attempt != nil && st.attempt.unsentAtBoundary != nil {
			delete(st.attempt.unsentAtBoundary, out.key)
		}
		st.inflight = nil
	case outBoundary:
		st.attempt = nil
		if !st.follow {
			st.closeLocked()
			return true
		}
	}
	return false
}

// recordTagged keeps the last available view written to a tagged stream, and
// re-sends it when its provider's visibility changed while it was written.
func (st *Stream) recordTagged(out output) {
	if !st.tagged {
		return
	}
	if !bool(out.view.Available) {
		delete(st.written, out.key)
		return
	}
	st.written[out.key] = out.view
	if out.open != st.c.isOpenKey(out.key) && st.follow {
		st.offer(out.key, out.view)
	}
}
