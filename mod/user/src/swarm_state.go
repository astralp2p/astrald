package user

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/objects"
	usermod "github.com/astralp2p/astrald/mod/user"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

const (
	// swarmStateDir holds one folder per user.EntryKind, each holding entries named by their entry ID.
	swarmStateDir = "/swarmstate"
	// swarmHeadPath holds SwarmHead in decimal. It lives outside swarmStateDir, so no sync ever reads it.
	swarmHeadPath = "/swarmhead"
	// swarmHeldLimit is the most entries held (user.EntryWait) at once.
	swarmHeldLimit = 1024
)

// swarmFiles is the part of vfs.Module that keeps the entries in the node's own VFS.
type swarmFiles interface {
	CreateVFS(id *astral.Identity) error
	WriteFile(ctx *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error)
	ReadFile(ctx *astral.Context, id *astral.Identity, path string) ([]byte, error)
	ListFolder(id *astral.Identity, path string) ([]string, error)
}

// swarmChecker decides on merge whether an entry joins the set.
type swarmChecker interface {
	CheckSwarmEntry(obj astral.Object) user.EntryVerdict
}

// swarmEntryPusher receives the entries this node passes on.
type swarmEntryPusher interface {
	SwarmPush(ctx *astral.Context, obj astral.Object) error
}

var (
	_ usermod.SwarmState = &swarmStore{}
	_ swarmFiles         = vfsmod.Module(nil)
	_ swarmChecker       = usermod.SwarmView(nil)
)

// swarmStore is this node's copy of SwarmState: a grow-only set of entries in the VFS of the node's identity.
//
// note: nothing constructs it in the running node yet; G7 does, and embeds usermod.SwarmState into Module.
type swarmStore struct {
	files   swarmFiles
	owner   *astral.Identity // the node identity, whose VFS holds the set
	checker swarmChecker
	ctx     *astral.Context // the node's own work: reads for SwarmEntry, head writes and passing entries on

	// stored indexes every stored entry ID by its kind.
	stored sig.Map[astral.ObjectID, user.EntryKind]
	head   atomic.Uint64

	merge swarmMerge

	// peers returns where an accepted entry is passed on; nil passes nothing on.
	peers func() []swarmEntryPusher
	// pushTimeout bounds one pass-on push; zero is swarmPushTimeout.
	pushTimeout time.Duration
	// passOnWait makes passOn return only once its pushes ended.
	// note: a test seam, so a test of the merge reads the peers' copies at once.
	passOnWait bool
}

// swarmMerge serializes merges and holds the entries that wait (user.EntryWait).
type swarmMerge struct {
	mu   sync.Mutex
	held []*swarmEntry // oldest first
}

// swarmEntry is an object user.EntryKindOf accepts, with its entry ID.
type swarmEntry struct {
	obj  astral.Object
	kind user.EntryKind
	id   astral.ObjectID
}

// newSwarmEntry returns obj as an entry, or an error for an object that is not one.
func newSwarmEntry(obj astral.Object) (*swarmEntry, error) {
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

	return &swarmEntry{obj: obj, kind: kind, id: *id}, nil
}

// path returns the path of the entry's file.
func (e *swarmEntry) path() string { return swarmEntryPath(e.kind, &e.id) }

func swarmEntryPath(kind user.EntryKind, id *astral.ObjectID) string {
	return swarmStateDir + "/" + kind.String() + "/" + id.String()
}

// newSwarmStore opens the set in the VFS of owner, creating the VFS when it is missing, and counts its entries.
func newSwarmStore(ctx *astral.Context, files swarmFiles, owner *astral.Identity, checker swarmChecker) (*swarmStore, error) {
	s := &swarmStore{files: files, owner: owner, checker: checker, ctx: ctx}

	if err := files.CreateVFS(owner); err != nil {
		return nil, fmt.Errorf("create the VFS of %v: %w", owner, err)
	}

	for _, kind := range user.EntryKinds {
		if err := s.loadKind(kind); err != nil {
			return nil, err
		}
	}

	s.head.Store(uint64(s.stored.Len()))

	// why: the count is recounted from the entries, so an entry stored before a failed head write still counts.
	if s.readHead() != s.head.Load() {
		s.writeHead()
	}

	return s, nil
}

// loadKind indexes the stored entries of one kind.
func (s *swarmStore) loadKind(kind user.EntryKind) error {
	names, err := s.files.ListFolder(s.owner, swarmStateDir+"/"+kind.String())
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("list %v: %w", kind, err)
	}

	for _, name := range names {
		id, err := astral.ParseID(name)
		if err != nil {
			// note: a file that is not named by an entry ID is not an entry, and is left alone.
			continue
		}
		s.stored.Set(*id, kind)
	}

	return nil
}

// readHead returns the count in the head file, or 0 when it cannot be read.
func (s *swarmStore) readHead() uint64 {
	data, err := s.files.ReadFile(objects.WithHousekeeping(s.ctx), s.owner, swarmHeadPath)
	if err != nil {
		return 0
	}

	n, _ := strconv.ParseUint(string(data), 10, 64)

	return n
}

// writeHead writes the count to the head file.
//
// note: a failed write is not an error of the merge: SwarmHead serves the count in memory, and the next open
// recounts it from the entries.
func (s *swarmStore) writeHead() {
	_, _ = s.files.WriteFile(s.ctx, s.owner, swarmHeadPath, []byte(strconv.FormatUint(s.head.Load(), 10)))
}

// AddSwarmEntry offers obj to the set (usermod.SwarmState). The entries it stores are passed on after the merge.
func (s *swarmStore) AddSwarmEntry(ctx *astral.Context, obj astral.Object) error {
	e, err := newSwarmEntry(obj)
	if err != nil {
		return err
	}

	accepted, err := s.mergeEntry(ctx, e)

	// why: outside the merge lock, so a peer that passes the entry straight back finds it stored, not a lock.
	s.passOn(accepted)

	return err
}

// mergeEntry runs the check on one entry and returns every entry the merge stored: the entry and the held
// entries it let in.
func (s *swarmStore) mergeEntry(ctx *astral.Context, e *swarmEntry) ([]astral.Object, error) {
	s.merge.mu.Lock()
	defer s.merge.mu.Unlock()

	// why: a duplicate is decided before the check, so it never reaches CheckSwarmEntry.
	if s.isStored(&e.id) {
		return nil, nil
	}

	switch s.checker.CheckSwarmEntry(e.obj) {
	case user.EntryAccept:
		if err := s.store(ctx, e); err != nil {
			return nil, err
		}
		s.unhold(&e.id)

		return append([]astral.Object{e.obj}, s.admitHeld(ctx)...), nil

	case user.EntryWait:
		s.hold(e)

		return nil, usermod.ErrSwarmEntryHeld

	default:
		// note: a verdict other than the three is a drop.
		s.unhold(&e.id)

		return nil, fmt.Errorf("%w: %v", usermod.ErrSwarmEntryDropped, &e.id)
	}
}

// admitHeld checks the held entries again until a pass admits none, and returns the entries it stored.
// The caller holds the merge lock.
func (s *swarmStore) admitHeld(ctx *astral.Context) (admitted []astral.Object) {
	for progress := true; progress; {
		progress = false

		for _, e := range append([]*swarmEntry(nil), s.merge.held...) {
			switch s.checker.CheckSwarmEntry(e.obj) {
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

// hold keeps an entry for the next merge. When the held set is full, the oldest entry is dropped.
// The caller holds the merge lock.
func (s *swarmStore) hold(e *swarmEntry) {
	for _, h := range s.merge.held {
		if h.id == e.id {
			return
		}
	}

	s.merge.held = append(s.merge.held, e)
	if len(s.merge.held) > swarmHeldLimit {
		s.merge.held = s.merge.held[1:]
	}
}

// unhold removes an entry from the held set. The caller holds the merge lock.
func (s *swarmStore) unhold(id *astral.ObjectID) {
	for i, h := range s.merge.held {
		if h.id == *id {
			s.merge.held = append(s.merge.held[:i:i], s.merge.held[i+1:]...)
			return
		}
	}
}

// store writes an accepted entry to its file, indexes it and grows the head. The caller holds the merge lock.
func (s *swarmStore) store(ctx *astral.Context, e *swarmEntry) error {
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, e.obj, astral.WithEncoder(astral.CanonicalTypeEncoder)); err != nil {
		return err
	}

	written, err := s.files.WriteFile(ctx, s.owner, e.path(), buf.Bytes())
	if err != nil {
		return fmt.Errorf("store %v: %w", &e.id, err)
	}

	// why: the file's content ID is the entry ID only when the bytes are the canonical form ResolveObjectID hashes.
	if *written != e.id {
		return fmt.Errorf("store %v: content stored as %v", &e.id, written)
	}

	s.stored.Set(e.id, e.kind)
	s.head.Add(1)
	s.writeHead()

	return nil
}

func (s *swarmStore) isStored(id *astral.ObjectID) bool {
	_, ok := s.stored.Get(*id)
	return ok
}

// swarmPushTimeout bounds the push of one entry to one peer.
// why: as user.PushToSiblings bounds a sibling push; a timed-out query also
// wakes the link's ping, which closes a dead link.
const swarmPushTimeout = 15 * time.Second

// passOn pushes each entry to every peer, each peer in its own goroutine and
// in the order of objs, each push bounded by swarmPushTimeout. It does not
// wait for the pushes. A peer that refuses or fails is left to repair itself
// by a pull.
//
// why: on the store's context, as passing on is this node's own work and not the work of whoever offered the entry.
// why not waiting: a push into a link that takes bytes and never answers
// blocked the writer, such as an answer op or a host signing an app's
// contract, until that link closed.
func (s *swarmStore) passOn(objs []astral.Object) {
	if len(objs) == 0 || s.peers == nil {
		return
	}

	timeout := s.pushTimeout
	if timeout <= 0 {
		timeout = swarmPushTimeout
	}

	var wg sync.WaitGroup
	for _, peer := range s.peers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, obj := range objs {
				ctx, cancel := s.ctx.WithTimeout(timeout)
				_ = peer.SwarmPush(ctx, obj)
				cancel()
			}
		}()
	}
	if s.passOnWait {
		wg.Wait()
	}
}

// SwarmEntry returns a stored entry decoded as its signed type (usermod.SwarmState).
func (s *swarmStore) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	if id == nil {
		return nil, errors.New("nil entry ID")
	}

	kind, ok := s.stored.Get(*id)
	if !ok {
		return nil, fmt.Errorf("swarm entry %v: %w", id, vfsapi.ErrNotFound)
	}

	// why housekeeping: readers poll the set, and a poll is this node's own bookkeeping, which leaves the
	// entries' last-touched times as they were (VFS 6, §11 "objects: last touched").
	data, err := s.files.ReadFile(objects.WithHousekeeping(s.ctx), s.owner, swarmEntryPath(kind, id))
	if err != nil {
		return nil, fmt.Errorf("read swarm entry %v: %w", id, err)
	}

	obj, _, err := astral.Decode(bytes.NewReader(data), astral.Canonical())
	if err != nil {
		return nil, fmt.Errorf("decode swarm entry %v: %w", id, err)
	}

	return obj, nil
}

// SwarmEntries returns the IDs of the stored entries of kind (usermod.SwarmState).
func (s *swarmStore) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if !kind.IsValid() {
		return nil, fmt.Errorf("invalid entry kind %q", kind)
	}

	var ids []*astral.ObjectID
	for id, k := range s.stored.Clone() {
		if k == kind {
			ids = append(ids, &id)
		}
	}

	return ids, nil
}

// SwarmHead returns the number of stored entries (usermod.SwarmState).
func (s *swarmStore) SwarmHead() uint64 {
	return s.head.Load()
}

// swarmDigest returns the IDs of every stored entry, of every kind.
func (s *swarmStore) swarmDigest() []*astral.ObjectID {
	keys := s.stored.Keys()

	ids := make([]*astral.ObjectID, 0, len(keys))
	for i := range keys {
		ids = append(ids, &keys[i])
	}

	return ids
}
