package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	objectsmod "github.com/cryptopunkscc/astrald/mod/objects"
)

type opCreateArgs struct {
	Alloc uint64 `query:"optional"`
	Repo  string `query:"optional"`
	In    string `query:"optional"`
	Out   string `query:"optional"`
}

// OpCreate creates a new object in the repository. It expects a stream of Blob objects followed by objects.Commit.
// On successful commit returns an ObjectID, an ErrorMessage otherwise; either response ends the op. Closing the
// connection before committing will discard the data.
func (mod *Module) OpCreate(ctx *astral.Context, q *routing.IncomingQuery, args opCreateArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var repo = mod.WriteDefault()

	// check repo
	if args.Repo != "" {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return ch.Send(astral.NewError("repository not found"))
		}
	}

	// create a new object in the repo
	w, err := repo.Create(ctx, &objectsmod.CreateOpts{Alloc: int(args.Alloc)})
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}
	defer w.Discard() // make sure we don't leave garbage behind

	// send an ack
	err = ch.Send(&astral.Ack{})
	if err != nil {
		return
	}

	return ch.Switch(
		func(blob *astral.Blob) (err error) {
			_, err = blob.WriteTo(w)
			return
		},
		func(*objects.CommitMsg) error {
			// the commit response ends the op either way - the writer cannot
			// accept blobs or a second commit past this point
			objectID, err := w.Commit()
			if err != nil {
				err = ch.Send(astral.NewError(err.Error()))
				if err != nil {
					return err
				}
				return channel.ErrBreak
			}

			mod.log.Logv(3, "%v created %v in %v", q.Caller(), objectID, repo)

			// seed dbObject: type isn't known from the raw blob stream, so
			// piggy-back on Probe — it reads the stamp from the just-written
			// repo and seeds via mod.trackObject. Errors are non-fatal; the
			// row will be lazily seeded by the next path that touches the object.
			_, perr := mod.Probe(ctx, repo, objectID)
			if perr != nil {
				mod.log.Logv(3, "OpCreate: probe-seed for %v failed: %v", objectID, perr)
			}

			err = ch.Send(objectID)
			if err != nil {
				return err
			}
			return channel.ErrBreak
		},
		channel.WithContext(ctx),
	)
}
