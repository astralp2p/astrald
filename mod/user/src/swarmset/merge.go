package swarmset

import (
	"bytes"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Added is the outcome of Add.
type Added struct {
	// Verdict is the entry check's verdict on the offered entry; a duplicate is user.EntryAccept.
	Verdict user.EntryVerdict
	// Stored are the entries the merge stored: the offered entry and the held entries it let in, in that order.
	// A duplicate stores nothing.
	Stored []astral.Object
}

// Add offers obj to the set. An entry already stored is a no-op, decided before any check. Otherwise it runs the
// entry check:
//   - user.EntryAccept: obj is stored, Head grows by one, and the held entries are checked again;
//   - user.EntryWait: obj is held unstored and checked again after the next merge. The held set is bounded by
//     heldLimit; when full, the oldest held entry is dropped first;
//   - user.EntryDrop: obj is not stored. A verdict other than the three is a drop.
//
// It returns an error for a non-entry, before Open and when the store fails.
func (s *Set) Add(ctx *astral.Context, obj astral.Object) (Added, error) {
	if !s.IsOpen() {
		return Added{}, ErrNotOpen
	}
	e, err := newEntry(obj)
	if err != nil {
		return Added{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// why: a duplicate is decided before the check, so it never reaches the Checker.
	if s.Has(&e.id) {
		return Added{Verdict: user.EntryAccept}, nil
	}

	switch v := s.cfg.Checker.Check(e.obj, s.reader(time.Now())); v {
	case user.EntryAccept:
		if err := s.store(ctx, e); err != nil {
			return Added{}, err
		}
		s.unhold(&e.id)
		return Added{Verdict: v, Stored: append([]astral.Object{e.obj}, s.admitHeld(ctx)...)}, nil

	case user.EntryWait:
		s.hold(e)
		return Added{Verdict: v}, nil

	default:
		s.unhold(&e.id)
		return Added{Verdict: user.EntryDrop}, nil
	}
}

// admitHeld checks the held entries again until a pass admits none, and returns the entries it stored. The
// caller holds the merge lock.
func (s *Set) admitHeld(ctx *astral.Context) (admitted []astral.Object) {
	for progress := true; progress; {
		progress = false

		for _, e := range append([]*entry(nil), s.held...) {
			switch s.cfg.Checker.Check(e.obj, s.reader(time.Now())) {
			case user.EntryWait:
				continue
			case user.EntryAccept:
				if err := s.store(ctx, e); err != nil {
					// note: it stays held, and the next merge tries it again.
					continue
				}
				admitted = append(admitted, e.obj)
				progress = true
			}

			s.unhold(&e.id)
		}
	}

	return admitted
}

// hold keeps an entry for the next merge. When the held set is full, the oldest entry is dropped. The caller
// holds the merge lock.
func (s *Set) hold(e *entry) {
	for _, h := range s.held {
		if h.id == e.id {
			return
		}
	}

	s.held = append(s.held, e)
	if len(s.held) > heldLimit {
		s.held = s.held[1:]
	}
}

// unhold removes an entry from the held set. The caller holds the merge lock.
func (s *Set) unhold(id *astral.ObjectID) {
	for i, h := range s.held {
		if h.id == *id {
			s.held = append(s.held[:i:i], s.held[i+1:]...)
			return
		}
	}
}

// store writes an accepted entry to its file, indexes it and grows the head. The caller holds the merge lock.
func (s *Set) store(ctx *astral.Context, e *entry) error {
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, e.obj, astral.WithEncoder(astral.CanonicalTypeEncoder)); err != nil {
		return err
	}

	written, err := s.files.WriteFile(ctx, s.owner, EntryPath(e.kind, &e.id), buf.Bytes())
	if err != nil {
		return fmt.Errorf("store %v: %w", &e.id, err)
	}

	// why: the file's content ID is the entry ID only when the bytes are the canonical form ResolveObjectID hashes.
	if *written != e.id {
		return fmt.Errorf("store %v: content stored as %v", &e.id, written)
	}

	s.index.Add(&e.id, e.obj)
	s.stored.Set(e.id, e.obj)
	s.head.Add(1)
	s.writeHead()

	return nil
}

// reader returns the Checker's read-only view of the index, with its clock at now. The caller holds the merge
// lock.
func (s *Set) reader(now time.Time) Reader {
	return indexReader{index: s.index, now: now}
}

// indexReader reads the set's index for the Checker.
type indexReader struct {
	index *Index
	now   time.Time
}

func (r indexReader) Entry(id *astral.ObjectID) (astral.Object, bool) {
	return r.index.Entry(id)
}

func (r indexReader) Snapshot(root *astral.Identity) *Snapshot {
	return &Snapshot{Index: r.index, Root: root, Now: r.now}
}
