package vfs

import (
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// tokenBucket holds the tokens of one VFS: bytes of directory objects the background compute may store.
//
// note: the bucket never goes into debt; a charge above the tokens empties it.
type tokenBucket struct {
	size   float64
	rate   float64 // tokens per second
	tokens float64
	at     time.Time // when tokens was last refilled
}

// refill adds the tokens earned since the last refill, up to the size.
func (b *tokenBucket) refill(now time.Time) {
	if now.After(b.at) {
		b.tokens = min(b.size, b.tokens+now.Sub(b.at).Seconds()*b.rate)
		b.at = now
	}
}

// readyAt returns when the bucket can pay cost. A cost above the size needs a full bucket.
func (b *tokenBucket) readyAt(cost uint64, now time.Time) time.Time {
	b.refill(now)

	short := min(float64(cost), b.size) - b.tokens
	if short <= 0 {
		return now
	}

	return now.Add(time.Duration(math.Ceil(short / b.rate * float64(time.Second))))
}

// charge takes n tokens, down to 0.
func (b *tokenBucket) charge(n uint64, now time.Time) {
	b.refill(now)
	b.tokens = max(0, b.tokens-float64(n))
}

// pendingVFS is a VFS with a cleared root ObjectID, waiting for the background compute.
type pendingVFS struct {
	id      *astral.Identity
	first   time.Time // when the VFS became pending, or the start of the last compute that left it pending
	last    time.Time // the last change
	retryAt time.Time // after a failed compute, the earliest retry; zero when none
	seq     uint64    // counts the changes, so an estimate made before one is not kept
	cost    uint64    // the estimate of the compute; 0 when unknown
	readyAt time.Time // when the bucket can pay cost
}

// dueVFS is a snapshot of a pending VFS that is due.
type dueVFS struct {
	ID      *astral.Identity
	Key     string
	Due     time.Time
	Seq     uint64
	Cost    uint64
	ReadyAt time.Time
}

// dirScheduler is the in-memory state of the background compute: the pending VFSes, their buckets and the pace of
// the node.
//
// why: one mutex guards the pending set, the buckets and the node pace, because a compute updates all of them at
// once.
// note: the state is never stored; at start every VFS with a cleared root is pending and every bucket is full.
type dirScheduler struct {
	mu       sync.Mutex
	cfg      Config
	pending  map[string]*pendingVFS
	buckets  map[string]*tokenBucket
	nodeNext time.Time     // the earliest start of the next background compute
	wake     chan struct{} // a new pending VFS
}

func newDirScheduler(cfg Config) *dirScheduler {
	return &dirScheduler{
		cfg:     cfg,
		pending: map[string]*pendingVFS{},
		buckets: map[string]*tokenBucket{},
		wake:    sig.New(),
	}
}

// ensure returns the pending entry of a VFS, made when missing with first and last at now. The caller holds mu.
func (s *dirScheduler) ensure(id *astral.Identity, now time.Time) *pendingVFS {
	key := id.String()
	if p := s.pending[key]; p != nil {
		return p
	}

	p := &pendingVFS{id: id, first: now, last: now}
	s.pending[key] = p

	select {
	case s.wake <- struct{}{}:
	default:
	}

	return p
}

// noteChange records a change of a VFS whose folder IDs were cleared.
func (s *dirScheduler) noteChange(id *astral.Identity, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.ensure(id, now)
	p.last, p.cost, p.readyAt = now, 0, time.Time{}
	p.seq++
}

// noteFailure keeps a VFS pending and retries it after the longest wait.
func (s *dirScheduler) noteFailure(id *astral.Identity, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.ensure(id, now)
	p.retryAt, p.cost, p.readyAt = now.Add(s.cfg.DirectoryLongestWait), 0, time.Time{}
}

// settle drops a VFS whose root ObjectID is cached.
//
// note: a caller holds the lock of the VFS, so no change of the VFS is noted between the check and the drop.
func (s *dirScheduler) settle(id *astral.Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pending, id.String())
}

// seed makes each VFS pending, as of now, that is not pending yet.
func (s *dirScheduler) seed(ids []*astral.Identity, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		s.ensure(id, now)
	}
}

// due returns when a pending VFS is computed: after the quiet time, at the latest after the longest wait, and not
// before a retry.
func (s *dirScheduler) due(p *pendingVFS) time.Time {
	quiet := p.last.Add(s.cfg.DirectoryQuietTime)
	longest := p.first.Add(s.cfg.DirectoryLongestWait)

	due := quiet
	if longest.Before(due) {
		due = longest
	}
	if p.retryAt.After(due) {
		due = p.retryAt
	}

	return due
}

// dueList returns the VFSes due at now, most overdue first, and the earliest due time of the others; zero when
// none.
func (s *dirScheduler) dueList(now time.Time) (ready []dueVFS, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, p := range s.pending {
		due := s.due(p)
		if due.After(now) {
			next = earliest(next, due)
			continue
		}
		ready = append(ready, dueVFS{ID: p.id, Key: key, Due: due, Seq: p.seq, Cost: p.cost, ReadyAt: p.readyAt})
	}

	// why: ties are ordered by identity, so the order never depends on the map.
	slices.SortFunc(ready, func(a, b dueVFS) int {
		if c := a.Due.Compare(b.Due); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})

	return ready, next
}

// bucket returns the bucket of a VFS, made full when missing. The caller holds mu.
func (s *dirScheduler) bucket(key string, now time.Time) *tokenBucket {
	b := s.buckets[key]
	if b == nil {
		size := float64(s.cfg.DirectoryBucketSize)
		b = &tokenBucket{size: size, rate: float64(s.cfg.DirectoryRefillRate), tokens: size, at: now}
		s.buckets[key] = b
	}

	return b
}

// bucketReadyAt returns when the bucket of a due VFS can pay cost, and keeps the estimate with the pending entry
// unless the VFS changed since the snapshot.
func (s *dirScheduler) bucketReadyAt(d dueVFS, cost uint64, now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	at := s.bucket(d.Key, now).readyAt(cost, now)
	if p := s.pending[d.Key]; p != nil && p.seq == d.Seq {
		p.cost, p.readyAt = cost, at
	}

	return at
}

// nodeReadyAt returns the earliest start of the next background compute.
func (s *dirScheduler) nodeReadyAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.nodeNext
}

// afterCompute charges a background compute to the bucket of its VFS and to the pace of the node. A VFS that is
// still pending waits the longest wait from the start of the compute at most.
//
// why: the node pace starts the next compute only after this one's bytes at the node rate, so the bytes written
// up to any time stay within the largest compute plus the rate times the time.
func (s *dirScheduler) afterCompute(id *astral.Identity, charged uint64, start, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := id.String()
	s.bucket(key, now).charge(charged, now)

	pace := time.Duration(float64(charged) / float64(s.cfg.DirectoryNodeRate) * float64(time.Second))
	s.nodeNext = later(s.nodeNext, now).Add(pace)

	if p := s.pending[key]; p != nil {
		p.first = later(p.first, start)
		p.cost, p.readyAt = 0, time.Time{}
	}
}

// earliest returns the earlier of two times; a zero time counts as none.
func earliest(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}

	return a
}

// later returns the later of two times.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
