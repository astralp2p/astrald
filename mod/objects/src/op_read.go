package objects

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
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
}

// OpRead authorizes the caller under SeeObjects, then streams raw object bytes
// over the accepted connection. With a repo it reads that repository. Without
// one it reads from the first candidate the finders name that delivers bytes.
// Records the access under the opened object's full ID in the reads journal,
// which feeds purge ordering.
//
// why every failure is a plain rejection (astral.CodeRejected): the answer is
// the object's raw bytes, which leave no room for an error_message, and shell
// pipes read it as the object itself.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (err error) {
	if args.Zone != nil {
		ctx = ctx.LimitZone(*args.Zone)
	}

	// why: a query from the network answers from this node alone. Its sender
	// already asks every candidate, and a second hop sends it back (objects.read spec).
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
		Repo:     astral.String8(args.Repo),
	}) {
		return q.Reject()
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).WithCancel()
	defer cancel()

	if len(args.Repo) > 0 {
		repo := mod.GetRepository(args.Repo)
		if repo == nil {
			return q.Reject()
		}

		r, err := repo.Read(ctx, args.ID, int64(args.Offset), int64(args.Limit))
		if err != nil {
			mod.log.Errorv(2, "read %v error: %v", args.ID, err)
			return q.Reject()
		}
		defer r.Close()

		// why: a partial request never becomes a journal key; the reader names the object it opened.
		mod.objectsReadsJournal.Mark(r.ID())

		conn := q.AcceptRaw()
		defer conn.Close()

		_, err = io.Copy(conn, r)
		return err
	}

	findCtx, cancelFind := ctx.WithTimeout(30 * time.Second)
	defer cancelFind()

	candidates, err := mod.Find(findCtx, args.ID)
	if err != nil {
		return q.Reject()
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
			return q.Reject()
		}
	}

	if won == nil {
		return q.Reject()
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

	conn := q.AcceptRaw()
	defer conn.Close()

	if _, err = conn.Write(won.head); err != nil {
		return err
	}

	// why: once bytes reach the caller, a source failure fails the read; no other source takes over.
	_, err = io.Copy(conn, won.r)
	return err
}
