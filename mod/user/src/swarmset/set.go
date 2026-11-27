// Package swarmset is the pure core of SwarmState: the grow-only set of signed entries in the VFS of the node's
// identity, its merge and held queue, the entry check (Rules) and the pure reading of the entries (Snapshot).
//
// It starts no goroutines and does no network or event work: mod/user passes entries on, syncs and emits events
// around it.
package swarmset

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// heldLimit is the most entries held (user.EntryWait) at once.
const heldLimit = 1024

// ErrNotOpen answers a call that needs the stored entries before Open.
var ErrNotOpen = errors.New("swarm state is not open yet")

// Config configures a Set.
type Config struct {
	// Checker is the entry check every entry passes before it is stored.
	Checker Checker
}

// Set is one node's copy of SwarmState: a grow-only set of entries, each stored at EntryPath in the VFS of the
// owning identity. Every stored entry passed the entry check: on merge, and again when the set opens.
type Set struct {
	cfg   Config
	files Files
	owner *astral.Identity
	ctx   *astral.Context // the node's own work: head writes and reads of the files

	open atomic.Bool
	// stored holds every stored entry by entry ID, read without the merge lock.
	stored sig.Map[astral.ObjectID, astral.Object]
	head   atomic.Uint64

	// mu is the merge lock: one merge, with its held re-check, at a time.
	mu sync.Mutex
	// index is the stored entries as the Checker reads them; guarded by mu.
	index *Index
	// held are the entries that wait, oldest first; guarded by mu.
	held []*entry
}

// New returns a closed set; Open opens it.
func New(cfg Config) *Set {
	return &Set{cfg: cfg, index: NewIndex()}
}

// entry is an object user.EntryKindOf accepts, with its entry ID.
type entry struct {
	obj  astral.Object
	kind user.EntryKind
	id   astral.ObjectID
}

// newEntry returns obj as an entry, or an error for an object that is not one.
func newEntry(obj astral.Object) (*entry, error) {
	if obj == nil {
		return nil, errors.New("nil swarm entry")
	}

	kind, ok := user.EntryKindOf(obj)
	if !ok {
		return nil, fmt.Errorf("%v is not a swarm entry", obj.ObjectType())
	}

	if n, ok := obj.(interface{ IsNil() bool }); ok && n.IsNil() {
		return nil, fmt.Errorf("empty %v", obj.ObjectType())
	}

	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return nil, err
	}

	return &entry{obj: obj, kind: kind, id: *id}, nil
}

// IsOpen reports whether Open has run.
func (s *Set) IsOpen() bool { return s.open.Load() }

// Entry returns the stored entry with entry ID id. Held entries are not returned.
func (s *Set) Entry(id *astral.ObjectID) (astral.Object, error) {
	if !s.IsOpen() {
		return nil, ErrNotOpen
	}
	if id == nil {
		return nil, errors.New("nil entry ID")
	}
	obj, ok := s.stored.Get(*id)
	if !ok {
		return nil, fmt.Errorf("swarm entry %v: %w", id, vfsapi.ErrNotFound)
	}
	return obj, nil
}

// Entries returns the entry IDs of the stored entries of kind, in no defined order; none before Open.
func (s *Set) Entries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if !kind.IsValid() {
		return nil, fmt.Errorf("invalid entry kind %q", kind)
	}

	var ids []*astral.ObjectID
	for id, obj := range s.stored.Clone() {
		if k, _ := user.EntryKindOf(obj); k == kind {
			ids = append(ids, &id)
		}
	}
	return ids, nil
}

// Head returns the number of stored entries; 0 before Open. It only grows.
func (s *Set) Head() uint64 { return s.head.Load() }

// Has reports whether an entry with entry ID id is stored.
func (s *Set) Has(id *astral.ObjectID) bool {
	_, ok := s.stored.Get(*id)
	return ok
}

// Digest returns the IDs of every stored entry, of every kind.
func (s *Set) Digest() []*astral.ObjectID {
	keys := s.stored.Keys()

	ids := make([]*astral.ObjectID, 0, len(keys))
	for i := range keys {
		ids = append(ids, &keys[i])
	}
	return ids
}

// readHead returns the count in the head file, or 0 when it cannot be read.
func (s *Set) readHead() uint64 {
	data, err := s.files.ReadFile(s.ctx, s.owner, HeadPath)
	if err != nil {
		return 0
	}

	n, _ := strconv.ParseUint(string(data), 10, 64)
	return n
}

// writeHead writes the count to the head file.
//
// note: a failed write is not an error of the merge: Head serves the count in memory, and the next open recounts
// it from the entries.
func (s *Set) writeHead() {
	_, _ = s.files.WriteFile(s.ctx, s.owner, HeadPath, []byte(strconv.FormatUint(s.head.Load(), 10)))
}

// decode decodes a stored file's content.
func decode(data []byte) (astral.Object, error) {
	obj, _, err := astral.Decode(bytes.NewReader(data), astral.Canonical())
	return obj, err
}
