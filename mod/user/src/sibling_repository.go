package user

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// siblingsRepo is the name the SiblingRepository is registered under.
const siblingsRepo = "siblings"

// siblingReadTimeout bounds routing one read to one sibling.
// why: a sibling that never answers must fail its attempt, so the next sibling is asked and the caller gets an answer.
const siblingReadTimeout = 15 * time.Second

var _ objectsmod.Repository = &SiblingRepository{}

// SiblingRepository reads objects from the device repositories of linked siblings.
// It is a member of the network group. It holds nothing and accepts no writes.
type SiblingRepository struct {
	mod *Module
}

func (repo *SiblingRepository) Label() string {
	return "Siblings"
}

// Read asks each linked sibling in turn for the object from its device repository,
// and returns the stream of the first sibling that accepts.
// A context without the network zone, no linked sibling, or a refusal from every sibling answers ErrNotFound.
func (repo *SiblingRepository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, objectsmod.ErrNotFound
	}

	// why: a sibling streams bytes alone, so a reader opened by a partial ID could not report the full ID it read.
	// note: the empty object's full ID has Size 0 as well, so it is refused too.
	if objectID.Size == 0 {
		return nil, objectsmod.ErrHashLookupUnsupported
	}

	for _, sibling := range repo.mod.getSiblings() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r, err := repo.readFrom(ctx, sibling, objectID, offset, limit)
		if err == nil {
			return r, nil
		}

		repo.mod.log.Logv(2, "read %v from sibling %v: %v", objectID, sibling, err)
	}

	return nil, objectsmod.ErrNotFound
}

// readFrom routes one objects.read for the sibling's device repository.
// why: the query is this node's, never the caller's. The caller was authorized on this node, and the sibling
// grants device reads to its siblings alone, so no app identity reaches the sibling.
// note: the stream RouteInFlight returns outlives its routing context, so the reader stays open after
// the Read context ends, which readConcurrent does on return.
func (repo *SiblingRepository) readFrom(ctx *astral.Context, sibling *astral.Identity, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	self := repo.mod.node.Identity()

	rctx, cancel := ctx.WithIdentity(self).WithTimeout(siblingReadTimeout)
	defer cancel()

	q := query.New(self, sibling, objects.MethodRead, query.Args{
		"id":     objectID,
		"offset": offset,
		"limit":  limit,
		"repo":   objects.RepoDevice,
	})

	conn, err := query.RouteInFlight(rctx, repo.mod.node, astral.Launch(q))
	if err != nil {
		return nil, err
	}

	return &siblingReader{ReadCloser: conn, repo: repo, id: *objectID}, nil
}

// Contains answers false. A sibling's object is never held here.
func (repo *SiblingRepository) Contains(*astral.Context, *astral.ObjectID) (bool, error) {
	return false, nil
}

// Scan emits no object. With follow, it emits the snapshot boundary and closes when ctx ends.
func (repo *SiblingRepository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID, 1)
	if !follow {
		close(ch)
		return ch, nil
	}

	ch <- nil
	context.AfterFunc(ctx, func() { close(ch) })

	return ch, nil
}

func (repo *SiblingRepository) Create(*astral.Context, *objectsmod.CreateOpts) (objects.Writer, error) {
	return nil, errors.ErrUnsupported
}

func (repo *SiblingRepository) Delete(*astral.Context, *astral.ObjectID) error {
	return errors.ErrUnsupported
}

// Free answers 0. The repository accepts no writes.
func (repo *SiblingRepository) Free(*astral.Context) (int64, error) {
	return 0, nil
}

// siblingReader is the stream of one object a sibling accepted to send.
type siblingReader struct {
	io.ReadCloser
	repo *SiblingRepository
	id   astral.ObjectID
}

func (r *siblingReader) Repo() objectsmod.Repository {
	return r.repo
}

func (r *siblingReader) ID() *astral.ObjectID {
	id := r.id
	return &id
}
