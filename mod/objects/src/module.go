package objects

import (
	"bytes"
	"fmt"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/nodes"
)

var _ objectsmod.Module = &Module{}

const defaultExternalDiscovererTimeout = 15 * time.Second

type Deps struct {
	Auth  auth.Module
	Dir   dir.Module
	Nodes nodes.Module
}

type Module struct {
	Deps
	node   astral.Node
	config Config
	db     *DB
	log    *log.Logger
	router routing.OpRouter

	ctx        *astral.Context
	system     objectsmod.Repository
	describers sig.Set[objects.Describer]
	searchers  sig.Set[objects.Searcher]
	searchPre  sig.Set[objects.SearchPreprocessor]
	finders    sig.Set[objects.Finder]
	receivers  sig.Set[objectsmod.Receiver]
	holders    sig.Set[objectsmod.Holder]
	indexers   sig.Set[objectsmod.Indexer]
	repos      sig.Map[string, objectsmod.Repository]

	externalMu sync.Mutex

	objectsReadsJournal *objectsReadsJournal
}

// Run blocks until ctx is cancelled, then flushes any pending object reads
// before returning.
func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx

	go mod.sweepExternalRegistrations(ctx)

	<-ctx.Done()

	err := mod.objectsReadsJournal.Flush()
	if err != nil {
		mod.log.Error("object reads journal: final flush: %v", err)
	}

	return nil
}

// Load reads and decodes an object from repo. Data that isn't a valid astral
// object is returned as an *astral.Blob rather than an error. An object larger
// than MaxObjectSize returns ErrObjectTooLarge. Marks the read in the reads
// journal and tracks the type for decoded objects, both under the opened
// object's full ID.
func (mod *Module) Load(ctx *astral.Context, repo objectsmod.Repository, objectID *astral.ObjectID) (astral.Object, error) {
	// read the object data
	r, err := repo.Read(ctx, objectID, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	data, err := readWhole(r)
	if err != nil {
		return nil, err
	}

	// why: a partial request never becomes a journal or tracking key; the reader names the object it opened.
	resolvedID := r.ID()

	mod.objectsReadsJournal.Mark(resolvedID)

	// parse the object
	o, _, err := astral.Decode(bytes.NewReader(data), astral.Canonical())
	switch {
	case err == nil:
		// decode succeeded, so the type is known.
		mod.trackObject(resolvedID, o.ObjectType())
		return o, nil

	case strings.Contains(err.Error(), "invalid magic bytes"): // the object is a blob
		// note: a blob reached only via Load is not seeded; objects without a
		// tracking row are not purge-eligible. accepted limitation for now.
		return (*astral.Blob)(&data), nil

	default: // other error
		return nil, err
	}
}

// readWhole reads the whole object r opened. An object larger than MaxObjectSize returns ErrObjectTooLarge.
func readWhole(r objectsmod.Reader) ([]byte, error) {
	// why: a partial request carries no size, so only the opened reader knows how large the object is.
	if r.ID().Size > uint64(objectsmod.MaxObjectSize) {
		return nil, objectsmod.ErrObjectTooLarge
	}

	// why: a reader can serve more bytes than its ID reports, so the read stops one byte past the cap.
	data, err := io.ReadAll(io.LimitReader(r, objectsmod.MaxObjectSize+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > objectsmod.MaxObjectSize {
		return nil, objectsmod.ErrObjectTooLarge
	}

	return data, nil
}

func (mod *Module) Store(ctx *astral.Context, repo objectsmod.Repository, object astral.Object) (*astral.ObjectID, error) {
	w, err := repo.Create(ctx, nil)
	if err != nil {
		return nil, err
	}

	_, err = astral.Encode(w, object, astral.WithEncoder(astral.CanonicalTypeEncoder))
	if err != nil {
		return nil, err
	}

	id, err := w.Commit()
	if err != nil {
		return nil, err
	}

	mod.trackObject(id, object.ObjectType())
	mod.index(object)

	return id, nil
}

// Probe reads only the object's header and reports its full ID, type, MIME,
// source repo, and read latency without loading the full payload. Tracks the
// type under the full ID when the object carries a valid astral stamp.
func (mod *Module) Probe(ctx *astral.Context, repo objectsmod.Repository, objectID *astral.ObjectID) (probe *objects.Probe, err error) {
	probe = &objects.Probe{}

	startAt := time.Now()

	// read the object data
	r, err := repo.Read(ctx, objectID, 0, 512)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// store the response time
	probe.Time = astral.Duration(time.Since(startAt))

	// store the actual repo name
	probe.Repo = astral.String8(mod.getRepoName(r.Repo()))

	// why: a partial request never becomes a tracking key; the reader names the object it opened.
	probe.ObjectID = r.ID()

	// check if it's an astral object
	q := bytes.NewReader(data)
	if _, err := (&astral.Stamp{}).ReadFrom(q); err == nil {
		var t astral.ObjectType
		_, err = t.ReadFrom(q)
		if err == nil {
			probe.Type = astral.String8(t.String())
			// seed dbObject: stamp+type parsed cleanly, type is in hand.
			// non-astral blobs fall through unseeded (same rationale as Load).
			mod.trackObject(probe.ObjectID, t.String())
		}
	}

	// check the mimeType
	probe.Mime = astral.String8(http.DetectContentType(data))

	return
}

func (mod *Module) AddSearchPreprocessor(pre objects.SearchPreprocessor) error {
	return mod.searchPre.Add(pre)
}

// trackObject seeds the dbObject row for an object the module just
// encountered. Failure is logged but not propagated — seeding is a side
// effect; the calling op must not fail because the cache write did.
// Re-seeding is harmless (db.Create is idempotent via INSERT OR IGNORE), so a
// missed seed is picked up the next time any path touches the same object.
func (mod *Module) trackObject(id *astral.ObjectID, objectType string) {
	err := mod.db.Create(id, objectType)
	if err != nil {
		mod.log.Error("track object %v: %v", id, err)
	}
}

// getRepoName returns the name of a repository
func (mod *Module) getRepoName(repo objectsmod.Repository) string {
	for name, r := range mod.repos.Clone() {
		if r == repo {
			return name
		}
	}
	return ""
}

func (mod *Module) Register(o astral.Object) (*astral.ObjectID, error) {
	return astral.DefaultBlueprints().Register(o)
}

// GetBlueprint returns the Blueprint for typeName: a runtime Blueprint as registered, or one
// derived from the compile-time prototype (alias kind for PrimitiveAlias prototypes, struct
// kind otherwise). Primitive names return astral.ErrPrimitiveType; unknown names return
// astral.ErrBlueprintNotFound. References inside the result are not resolved — the caller
// fetches referenced types itself.
func (mod *Module) GetBlueprint(typeName string) (*astral.Blueprint, error) {
	// why: primitives are registered under their own names, so New would hand back the
	// primitive prototype and BlueprintOf would fail with an opaque reflection error;
	// primitives have no blueprint by design, so reject them explicitly.
	if astral.IsPrimitiveType(typeName) {
		return nil, fmt.Errorf("%w: %s", astral.ErrPrimitiveType, typeName)
	}

	if bp := astral.DefaultBlueprints().GetBlueprint(typeName); bp != nil {
		return bp, nil
	}

	proto := astral.New(typeName)
	if proto == nil {
		return nil, fmt.Errorf("%w: %s", astral.ErrBlueprintNotFound, typeName)
	}

	return astral.BlueprintOf(proto)
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return objects.ModuleName
}

func containsSourceIdentity[T comparable](set *sig.Set[T], id *astral.Identity) bool {
	for _, item := range set.Clone() {
		sourceID, ok, err := objectsmod.SourceIdentity(item)
		if err != nil || !ok {
			continue
		}

		if sourceID.IsEqual(id) {
			return true
		}
	}

	return false
}
