package objects

import (
	"errors"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"sync"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

type RepoGroup struct {
	// Concurrent makes Read race all members and return the first hit; otherwise members are tried in order.
	Concurrent bool
	mod        *Module
	label      string
	repos      sig.Set[string]
}

var _ objectsmod.Repository = &RepoGroup{}

func NewRepoGroup(mod *Module, label string, concurrent bool) *RepoGroup {
	return &RepoGroup{
		mod:        mod,
		label:      label,
		Concurrent: concurrent,
	}
}

func (group *RepoGroup) Label() string {
	return group.label
}

func (group *RepoGroup) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	var errs []error

	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}
		w, err := repo.Create(ctx, opts)
		if err == nil {
			return w, nil
		}
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil, errors.New("repository group empty")
	}

	return nil, errors.Join(errs...)
}

// Contains reports whether a member contains the object. A group that finds nothing answers false, nil,
// ErrHashLookupUnsupported when every invoked member returned it, or the context error once ctx has ended.
func (group *RepoGroup) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	var tally lookupTally
	for _, repo := range group.members() {
		if ctx.Err() != nil {
			break
		}
		has, err := repo.Contains(ctx, objectID)
		if err == nil && has {
			return true, nil
		}
		tally.add(err)
	}
	return false, tally.missErr(ctx, nil)
}

// Scan merges the object streams of all members.
// With follow, a nil sentinel is emitted once every member has drained its backlog, then live updates continue.
func (group *RepoGroup) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID)

	go func() {
		defer close(ch)

		ctx, cancel := ctx.WithCancel()
		defer cancel()

		var sources []<-chan *astral.ObjectID

		for _, repoName := range group.repos.Clone() {
			repo := group.mod.GetRepository(repoName)
			if repo == nil {
				continue
			}

			sub, err := repo.Scan(ctx, follow)
			if err != nil {
				continue
			}

			sources = append(sources, sub)
		}

		if !follow {
			var wg sync.WaitGroup

			for _, source := range sources {
				wg.Add(1)
				go func(source <-chan *astral.ObjectID) {
					defer wg.Done()

					for id := range source {
						if id == nil {
							continue
						}

						select {
						case <-ctx.Done():
							return
						case ch <- id:
						}
					}
				}(source)
			}

			wg.Wait()
			return
		}

		var wg sync.WaitGroup
		boundaries := make(chan bool, len(sources))

		for _, source := range sources {
			wg.Add(1)
			go func(source <-chan *astral.ObjectID) {
				defer wg.Done()

				var id *astral.ObjectID
				var ok bool

				for {
					select {
					case <-ctx.Done():
						return
					case id, ok = <-source:
						if !ok {
							boundaries <- false
							cancel()
							return
						}
					}

					if id == nil {
						boundaries <- true
						return
					}

					select {
					case <-ctx.Done():
						return
					case ch <- id:
					}
				}
			}(source)
		}

		wg.Wait()
		close(boundaries)

		for ok := range boundaries {
			if !ok {
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case ch <- nil:
		}

		wg = sync.WaitGroup{}

		for _, source := range sources {
			wg.Add(1)
			go func(source <-chan *astral.ObjectID) {
				defer wg.Done()

				for {
					var id *astral.ObjectID
					var ok bool

					select {
					case <-ctx.Done():
						return
					case id, ok = <-source:
						if !ok {
							return
						}
					}

					if id == nil {
						cancel()
						return
					}

					select {
					case <-ctx.Done():
						return
					case ch <- id:
					}
				}
			}(source)
		}

		wg.Wait()
	}()

	return ch, nil
}

// Delete deletes the object from every member and succeeds when one member deletes it.
// A group that deletes nothing answers ErrNotFound, ErrHashLookupUnsupported when every invoked member returned it,
// or the context error once ctx has ended.
func (group *RepoGroup) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	var tally lookupTally
	var deleted bool
	for _, repo := range group.members() {
		if ctx.Err() != nil {
			break
		}
		err := repo.Delete(ctx, objectID)
		deleted = deleted || err == nil
		tally.add(err)
	}
	// why: a deletion that happened is a success even after ctx ended; the members not reached keep the object.
	if deleted {
		return nil
	}
	return tally.missErr(ctx, objectsmod.ErrNotFound)
}

// Read returns the reader of a member's hit unchanged. A group that finds nothing answers ErrNotFound,
// ErrHashLookupUnsupported when every invoked member returned it, or the context error once ctx has ended.
func (group *RepoGroup) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if group.Concurrent {
		return group.readConcurrent(ctx, objectID, offset, limit)
	}

	return group.readSeq(ctx, objectID, offset, limit)
}

func (group *RepoGroup) readSeq(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	var tally lookupTally
	for _, repo := range group.members() {
		if ctx.Err() != nil {
			break
		}
		r, err := repo.Read(ctx, objectID, offset, limit)
		if err == nil {
			return r, nil
		}
		tally.add(err)
	}
	return nil, tally.missErr(ctx, objectsmod.ErrNotFound)
}

// readResult is one member's answer to a concurrent read.
type readResult struct {
	reader objectsmod.Reader
	err    error
}

// readConcurrent races all members and returns the first hit. The readers of other hits are closed.
func (group *RepoGroup) readConcurrent(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ctx, cancel := ctx.WithCancel()
	defer cancel()

	var members = group.members()
	// why: the buffer holds every member's result, so a worker never blocks after the collector returns.
	var results = make(chan readResult, len(members))
	for _, repo := range members {
		go func() {
			r, err := repo.Read(ctx, objectID, offset, limit)
			results <- readResult{reader: r, err: err}
		}()
	}

	var tally lookupTally
	for pending := len(members); pending > 0; pending-- {
		res, err := sig.Recv(ctx, results)
		if err != nil {
			go closeReaders(results, pending)
			return nil, err
		}
		if res.err == nil {
			go closeReaders(results, pending-1)
			return res.reader, nil
		}
		tally.add(res.err)
	}

	return nil, tally.missErr(ctx, objectsmod.ErrNotFound)
}

// closeReaders receives n results and closes the reader of every hit among them.
func closeReaders(results <-chan readResult, n int) {
	for ; n > 0; n-- {
		if res := <-results; res.err == nil {
			res.reader.Close()
		}
	}
}

// members returns the member repositories in order. A member name with no registered repository is skipped.
func (group *RepoGroup) members() []objectsmod.Repository {
	var repos []objectsmod.Repository
	for _, repoName := range group.repos.Clone() {
		if repo := group.mod.GetRepository(repoName); repo != nil {
			repos = append(repos, repo)
		}
	}
	return repos
}

// lookupTally counts the members one group lookup invoked, and the members among them that cannot look up by hash.
// note: the goroutine that collects the members' answers owns the tally; workers never update it.
type lookupTally struct {
	invoked     int
	unsupported int
}

// add counts one invoked member by its answer. A nil error is a supported answer.
// note: a member's ErrAmbiguousObjectID counts as a miss, so ambiguity stays one repository's verdict.
func (tally *lookupTally) add(err error) {
	tally.invoked++
	if errors.Is(err, objectsmod.ErrHashLookupUnsupported) {
		tally.unsupported++
	}
}

// missErr returns the answer of a lookup that no member answered: the context error,
// ErrHashLookupUnsupported, or miss.
// why: a member that failed because the context ended did not miss, so an outer group must not answer a miss.
// why: one supported miss makes the group miss; the group cannot look up by hash only when no invoked member can.
func (tally *lookupTally) missErr(ctx *astral.Context, miss error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tally.invoked > 0 && tally.unsupported == tally.invoked {
		return objectsmod.ErrHashLookupUnsupported
	}
	return miss
}

func (group *RepoGroup) Free(ctx *astral.Context) (int64, error) {
	var total int64

	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}
		size, err := repo.Free(ctx)
		if err != nil {
			return 0, err
		}
		if size > 0 { // size might be -1 for unknown
			total += size
		}
	}

	return total, nil
}

func (group *RepoGroup) Add(repo string) error {
	if group.mod.GetRepository(repo) == nil {
		return errors.New("repository " + repo + " not found")
	}

	return group.repos.Add(repo)
}

func (group *RepoGroup) Remove(repo string) error {
	return group.repos.Remove(repo)
}

func (group *RepoGroup) List() []string {
	return group.repos.Clone()
}

func (group *RepoGroup) String() string {
	return group.label
}
