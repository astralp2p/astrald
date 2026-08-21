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

func (group *RepoGroup) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}
		if has, err := repo.Contains(ctx, objectID); err == nil && has {
			return true, nil
		}
	}
	return false, nil
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

func (group *RepoGroup) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	var count int
	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}
		if err := repo.Delete(ctx, objectID); err == nil {
			count++
		}
	}
	if count == 0 {
		return objectsmod.ErrNotFound
	}
	return nil
}

func (group *RepoGroup) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if group.Concurrent {
		return group.readConcurrent(ctx, objectID, offset, limit)
	}

	return group.readSeq(ctx, objectID, offset, limit)
}

func (group *RepoGroup) readSeq(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}
		r, err := repo.Read(ctx, objectID, offset, limit)
		if err == nil {
			return r, nil
		}
	}
	return nil, objectsmod.ErrNotFound
}

func (group *RepoGroup) readConcurrent(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	ctx, cancel := ctx.WithCancel()
	defer cancel()

	var res = make(chan objectsmod.Reader)
	var wg sync.WaitGroup

	for _, repoName := range group.repos.Clone() {
		repo := group.mod.GetRepository(repoName)
		if repo == nil {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := repo.Read(ctx, objectID, offset, limit)
			if err == nil {
				select {
				case res <- conn:
				case <-ctx.Done():
				}
				return
			}
		}()
	}

	go func() {
		wg.Wait()
		close(res)
	}()

	select {
	case r, ok := <-res:
		if ok {
			return r, nil
		}

		return nil, objectsmod.ErrNotFound

	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
