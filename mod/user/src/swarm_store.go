package user

import (
	"fmt"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

// errSwarmStateNotOpen answers a SwarmState call made before Run opened the store.
var errSwarmStateNotOpen = swarmset.ErrNotOpen

// swarmEntryPusher receives the entries this node passes on.
type swarmEntryPusher interface {
	SwarmPush(ctx *astral.Context, obj astral.Object) error
}

var _ swarmset.Files = vfsmod.Module(nil)

// swarmStore is this node's copy of SwarmState: the set (swarmset.Set) and the work around its merges that the
// pure set does not do, passing stored entries on to peers and reporting them.
type swarmStore struct {
	set *swarmset.Set
	ctx *astral.Context // the node's own work: passing entries on

	// peers returns where a stored entry is passed on; nil passes nothing on.
	peers func() []swarmEntryPusher
	// pushTimeout bounds one pass-on push; zero is swarmPushTimeout.
	pushTimeout time.Duration
	// passOnWait makes passOn return only once its pushes ended.
	// note: a test seam, so a test of the merge reads the peers' copies at once.
	passOnWait bool
	// stored runs after every merge that stored entries, outside the merge lock; nil does nothing.
	stored func(objs []astral.Object)
}

// newSwarmStore returns a closed store whose merges run checker. log reports what open leaves out; nil discards.
func newSwarmStore(checker swarmset.Checker, log func(format string, v ...any)) *swarmStore {
	return &swarmStore{set: swarmset.New(swarmset.Config{Checker: checker, Log: log})}
}

// open opens the set in the VFS of owner and checks every stored entry again (swarmset.Set.Open).
func (s *swarmStore) open(ctx *astral.Context, files swarmset.Files, owner *astral.Identity) error {
	s.ctx = ctx
	return s.set.Open(ctx, housekeepingFiles{files}, owner)
}

// housekeepingFiles reads the set's files as housekeeping.
//
// why: readers poll the set, and a poll is this node's own bookkeeping, which leaves the entries' last-touched
// times as they were (VFS 6, §11 "objects: last touched").
type housekeepingFiles struct{ swarmset.Files }

func (f housekeepingFiles) ReadFile(ctx *astral.Context, id *astral.Identity, path string) ([]byte, error) {
	return f.Files.ReadFile(objects.WithHousekeeping(ctx), id, path)
}

// merge offers obj to the set: the one merge every write and every sync ends in. The entries it stores are
// passed on and reported after the merge. A held entry returns usermod.ErrSwarmEntryHeld, and a dropped one an
// error wrapping usermod.ErrSwarmEntryDropped.
func (s *swarmStore) merge(ctx *astral.Context, obj astral.Object) error {
	added, err := s.set.Add(ctx, obj)
	if err != nil {
		return err
	}

	// why: outside the merge lock, so a peer that passes the entry straight back finds it stored, not a lock.
	s.passOn(added.Stored)
	if len(added.Stored) > 0 && s.stored != nil {
		s.stored(added.Stored)
	}

	switch added.Verdict {
	case user.EntryAccept:
		return nil
	case user.EntryWait:
		return usermod.ErrSwarmEntryHeld
	}
	id, _ := astral.ResolveObjectID(obj)
	return fmt.Errorf("%w: %v", usermod.ErrSwarmEntryDropped, id)
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

// SwarmEntry returns a stored entry decoded as its signed type.
func (s *swarmStore) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	return s.set.Entry(id)
}

// SwarmEntries returns the IDs of the stored entries of kind.
func (s *swarmStore) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	return s.set.Entries(kind)
}

// SwarmHead returns the number of stored entries.
func (s *swarmStore) SwarmHead() uint64 {
	return s.set.Head()
}

// swarmDigest returns the IDs of every stored entry, of every kind.
func (s *swarmStore) swarmDigest() []*astral.ObjectID {
	return s.set.Digest()
}

func (s *swarmStore) isStored(id *astral.ObjectID) bool {
	return s.set.Has(id)
}
