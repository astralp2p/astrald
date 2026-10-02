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
	outBoundary
	outView
)

type output struct {
	kind       outKind
	key        offeringKey
	view       *services.Update
	removed    []offeringKey
	incomplete *services.Incomplete
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
		out := st.next()
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
		done := st.sent(out)
		c.mu.Unlock()
		if done {
			return nil
		}
	}
}

// next picks what to send: removals, then the boundary once nothing owed is
// left, then views in first-pending order.
func (st *Stream) next() output {
	if len(st.removed) > 0 {
		keys := make([]offeringKey, 0, len(st.removed))
		for k := range st.removed {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].provider+"\x00"+keys[i].name < keys[j].provider+"\x00"+keys[j].name
		})
		return output{kind: outRemoved, removed: keys}
	}
	if at := st.attempt; at != nil && at.resolved && len(at.owed) == 0 {
		return output{kind: outBoundary, incomplete: at.incomplete()}
	}
	if len(st.queue) > 0 {
		k := st.queue[0]
		st.queue = st.queue[1:]
		v := st.pending[k]
		delete(st.pending, k)
		st.inflight = &k
		st.inflightAvailable = bool(v.Available)
		return output{kind: outView, key: k, view: v}
	}
	return output{}
}

func (out output) write(sink Sink) error {
	switch out.kind {
	case outRemoved:
		r := &services.Removed{}
		for _, k := range out.removed {
			id, err := astral.ParseIdentity(k.provider)
			if err != nil {
				return err
			}
			r.Offerings = append(r.Offerings, &services.OfferingKey{ProviderID: id, Name: astral.String8(k.name)})
		}
		return sink.Removed(r)
	case outBoundary:
		return sink.Boundary(out.incomplete)
	default:
		return sink.Update(out.view)
	}
}

// sent records a successful write and reports whether the stream is done.
func (st *Stream) sent(out output) bool {
	switch out.kind {
	case outRemoved:
		for _, k := range out.removed {
			delete(st.removed, k)
			delete(st.visible, k)
		}
	case outView:
		if bool(out.view.Available) {
			st.visible[out.key] = struct{}{}
		} else {
			delete(st.visible, out.key)
		}
		if st.attempt != nil && st.attempt.owed != nil {
			delete(st.attempt.owed, out.key)
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
