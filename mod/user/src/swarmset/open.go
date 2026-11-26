package swarmset

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// Open opens the set in the VFS of owner, creating the VFS when it is missing, and checks every stored entry
// again: it decodes it, confirms that its content ID equals its file name and that its kind is its folder's,
// and runs the entry check. An entry that fails is left out of the set and logged; its file stays. ctx is the
// node's own work: it carries every later head write and read of the files.
//
// why the check runs as of no time and with expels and Declines last: an entry that was valid when it arrived
// stays valid, and expiry, Declines and expels apply when entries are read; the check on open asks only whether
// the entry could have entered.
func (s *Set) Open(ctx *astral.Context, files Files, owner *astral.Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsOpen() {
		return errors.New("swarm state is already open")
	}
	if err := files.CreateVFS(owner); err != nil {
		return fmt.Errorf("create the VFS of %v: %w", owner, err)
	}
	s.files, s.owner, s.ctx = files, owner, ctx

	loaded := map[user.EntryKind][]*entry{}
	for _, kind := range user.EntryKinds {
		list, err := s.loadKind(kind)
		if err != nil {
			return err
		}
		loaded[kind] = list
	}

	var asStored time.Time
	s.admit(append(append(loaded[user.EntryRequests], loaded[user.EntryApprovals]...),
		loaded[user.EntryContracts]...), asStored)
	s.admit(loaded[user.EntryDeclines], asStored)
	s.admit(loaded[user.EntryExpels], asStored)

	s.head.Store(uint64(s.stored.Len()))
	// why: the count is recounted from the entries, so an entry stored before a failed head write still counts.
	if s.readHead() != s.head.Load() {
		s.writeHead()
	}

	s.open.Store(true)
	return nil
}

// loadKind reads the stored entries of one kind. A file that is not named by an entry ID is not an entry and is
// left alone; one that does not decode as an entry of kind with that ID is logged and left out.
func (s *Set) loadKind(kind user.EntryKind) ([]*entry, error) {
	dir := Dir + "/" + kind.String()
	names, err := s.files.ListFolder(s.owner, dir)
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("list %v: %w", kind, err)
	}

	var list []*entry
	for _, name := range names {
		id, err := astral.ParseID(name)
		if err != nil {
			continue
		}
		e, err := s.loadEntry(kind, id)
		if err != nil {
			s.cfg.Log("swarm state: %v/%v left out: %v", kind, name, err)
			continue
		}
		list = append(list, e)
	}
	return list, nil
}

// loadEntry reads and decodes the stored entry of kind with entry ID id.
func (s *Set) loadEntry(kind user.EntryKind, id *astral.ObjectID) (*entry, error) {
	data, err := s.files.ReadFile(s.ctx, s.owner, EntryPath(kind, id))
	if err != nil {
		return nil, err
	}
	obj, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	e, err := newEntry(obj)
	switch {
	case err != nil:
		return nil, err
	case e.kind != kind:
		return nil, fmt.Errorf("a %v entry in the %v folder", e.kind, kind)
	case e.id != *id:
		return nil, fmt.Errorf("content ID %v", &e.id)
	}
	return e, nil
}

// admit runs the entry check on loaded entries until a pass admits none, with the Reader's clock at now, and
// indexes each one it accepts. The rest are logged and left out. The caller holds the merge lock.
func (s *Set) admit(pending []*entry, now time.Time) {
	for progress := true; progress && len(pending) > 0; {
		progress = false
		var waiting []*entry
		for _, e := range pending {
			switch s.cfg.Checker.Check(e.obj, s.reader(now)) {
			case user.EntryAccept:
				s.index.Add(&e.id, e.obj)
				s.stored.Set(e.id, e.obj)
				progress = true
			case user.EntryWait:
				waiting = append(waiting, e)
			default:
				s.cfg.Log("swarm state: %v/%v left out: the entry check drops it", e.kind, &e.id)
			}
		}
		pending = waiting
	}
	for _, e := range pending {
		s.cfg.Log("swarm state: %v/%v left out: what it needs is not stored", e.kind, &e.id)
	}
}
