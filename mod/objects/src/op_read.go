package objects

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/core"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opReadArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Offset astral.Uint64
	Limit  astral.Uint64
	Zone   *astral.Zone
	Repo   string
	Out    string
}

// readChunkSize is the most bytes one bytes32 chunk of a framed read carries.
const readChunkSize = 32 * 1024

// OpRead authorizes the caller under SeeObjects and reads the object. With a
// repo it reads that repository. Without one it reads from the first candidate
// the finders name that delivers bytes. Records the access under the opened
// object's full ID in the reads journal, which feeds purge ordering.
//
// Without out it streams raw object bytes, and every failure is a rejection.
// With out it accepts first and answers on a channel: one error_message, or the
// bytes as bytes32 chunks ended by eos.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (err error) {
	if args.Zone != nil {
		ctx = ctx.LimitZone(*args.Zone)
	}

	// why: a query from the network answers from this node alone. Its sender
	// already asks every candidate, and a second hop sends it back (objects.read spec).
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	if args.Out == "" {
		return mod.readRaw(ctx, q, args)
	}
	return mod.readFramed(ctx, q, args)
}

// readRaw answers the raw mode: the object's bytes with no framing.
func (mod *Module) readRaw(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) error {
	src, err := mod.openRead(ctx, q, args)
	if err != nil {
		// note: raw bytes leave no room for an error_message, so every failure is the generic rejection.
		return q.Reject()
	}
	defer src.close()

	conn := q.AcceptRaw()
	defer conn.Close()

	// why: once bytes reach the caller, a source failure fails the read; no other source takes over.
	_, err = io.Copy(conn, src)
	return err
}

// readFramed answers the framed mode: one error_message, or the object's bytes
// as bytes32 chunks ended by eos.
func (mod *Module) readFramed(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	src, err := mod.openRead(ctx, q, args)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	defer src.close()

	buf := make([]byte, readChunkSize)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			chunk := astral.Bytes32(buf[:n])
			if sendErr := ch.Send(&chunk); sendErr != nil {
				return sendErr
			}
		}
		switch {
		case errors.Is(err, io.EOF):
			return ch.Send(&astral.EOS{})
		case err != nil:
			// why: once bytes reach the caller, a source failure fails the read; no other source takes over.
			return ch.Send(astral.Err(err))
		}
	}
}

// readSource is the bytes a read delivers and what to release once they are read.
type readSource struct {
	io.Reader
	close func()
}

// openRead authorizes the caller and opens the object, from the named
// repository or from the first candidate that delivers bytes. It answers
// auth.ErrNotPermitted, objects.ErrRepositoryNotFound, objects.ErrNotFound, or
// the read or lookup error.
func (mod *Module) openRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (*readSource, error) {
	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
		Repo:     astral.String8(args.Repo),
	}) {
		return nil, auth.ErrNotPermitted
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).WithCancel()

	var src *readSource
	var err error
	if len(args.Repo) > 0 {
		src, err = mod.openRepo(ctx, args)
	} else {
		src, err = mod.openCandidates(ctx, args)
	}
	if err != nil {
		cancel()
		return nil, err
	}

	closeSource := src.close
	src.close = func() {
		closeSource()
		cancel()
	}
	return src, nil
}

// openRepo opens the object in the repository args name.
func (mod *Module) openRepo(ctx *astral.Context, args opReadArgs) (*readSource, error) {
	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return nil, objects.ErrRepositoryNotFound
	}

	r, err := repo.Read(ctx, args.ID, int64(args.Offset), int64(args.Limit))
	if err != nil {
		mod.log.Errorv(2, "read %v error: %v", args.ID, err)
		return nil, err
	}

	// why: a partial request never becomes a journal key; the reader names the object it opened.
	mod.objectsReadsJournal.Mark(r.ID())

	return &readSource{Reader: r, close: func() { r.Close() }}, nil
}

// openCandidates opens the object from the first candidate the finders name
// that delivers bytes, or ends cleanly with none.
func (mod *Module) openCandidates(ctx *astral.Context, args opReadArgs) (*readSource, error) {
	findCtx, cancelFind := ctx.WithTimeout(30 * time.Second)
	defer cancelFind()

	candidates, err := mod.Find(findCtx, args.ID)
	if err != nil {
		return nil, err
	}

	type attempt struct {
		key  string
		r    io.ReadCloser
		head []byte
		err  error
	}

	// why: candidates are tried concurrently as they arrive, with no priority
	// between local and remote; the first to deliver bytes supplies the read.
	results := make(chan attempt)
	cancels := make(map[string]context.CancelFunc)
	pending := 0
	var won *attempt

	for won == nil && (candidates != nil || pending > 0) {
		select {
		case source, ok := <-candidates:
			if !ok {
				candidates = nil
				continue
			}
			if _, seen := cancels[source.String()]; seen {
				continue
			}

			attemptCtx, cancelAttempt := ctx.WithCancel()
			cancels[source.String()] = cancelAttempt
			pending++

			go func() {
				a := attempt{key: source.String()}
				if source.IsEqual(mod.node.Identity()) {
					a.r, a.err = mod.ReadDefault().Read(attemptCtx.ExcludeZone(astral.ZoneNetwork), args.ID, int64(args.Offset), int64(args.Limit))
				} else {
					// why: the host authorized its caller; the source serves the host as
					// itself and trusts that decision (objects.read spec).
					// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
					// note: the query names no out, so the source answers in the raw mode.
					a.r, a.err = core.NewClientAs(mod.node, mod.node.Identity()).WithTarget(source).Query(
						attemptCtx.WithIdentity(mod.node.Identity()), objects.MethodRead, query.Args{
							"id":     args.ID,
							"offset": int64(args.Offset),
							"limit":  int64(args.Limit),
							"zone":   astral.ZoneDevice | astral.ZoneVirtual,
						})
				}

				if a.err == nil {
					// why: a blocked Read returns only when its source closes; the reader
					// closes when its attempt is cancelled or the read ends.
					context.AfterFunc(attemptCtx, func() { a.r.Close() })
					timer := time.AfterFunc(15*time.Second, cancelAttempt)

					buf := make([]byte, 32*1024)
					n, err := a.r.Read(buf)
					switch {
					case !timer.Stop():
						a.err = context.DeadlineExceeded
					case n > 0:
						a.head = buf[:n]
					case errors.Is(err, io.EOF):
						// why: a clean end with no bytes is a valid empty read.
						a.head = buf[:0]
					case err != nil:
						a.err = err
					default:
						a.err = io.ErrNoProgress
					}
				}

				if a.err != nil {
					mod.log.Logv(1, "read %v from %v: %v", args.ID, source, a.err)
					cancelAttempt()
				}

				select {
				case results <- a:
				case <-ctx.Done():
				}
			}()

		case a := <-results:
			pending--
			if a.err == nil {
				won = &a
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if won == nil {
		return nil, objects.ErrNotFound
	}

	// why: the winner stays open until the read ends; discovery and every other attempt stop now.
	cancelFind()
	for key, cancelAttempt := range cancels {
		if key != won.key {
			cancelAttempt()
		}
	}

	if r, ok := won.r.(objectsmod.Reader); ok {
		// why: a partial request never becomes a journal key; the reader names the object it opened.
		mod.objectsReadsJournal.Mark(r.ID())
	}

	return &readSource{
		Reader: io.MultiReader(bytes.NewReader(won.head), won.r),
		close:  func() { won.r.Close() },
	}, nil
}
