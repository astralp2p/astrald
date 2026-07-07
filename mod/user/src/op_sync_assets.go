package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opSyncAssetsArgs struct {
	Start astral.Uint64 `query:"optional"`
	Out   string        `query:"optional"`
}

// OpSyncAssets streams asset delta records from the given DB height and replies with the next unread height.
// If no rows are found at or above Start, echoes Start as the height so callers can safely re-poll.
func (mod *Module) OpSyncAssets(ctx *astral.Context, q *routing.IncomingQuery, args opSyncAssetsArgs) (err error) {
	var rows []*dbAsset

	err = mod.db.Where("height >= ?", args.Start).Find(&rows).Error
	if err != nil {
		mod.log.Error("db error: %v", err)
		return q.RejectWithCode(2)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var height astral.Uint64

	if len(rows) == 0 {
		height = args.Start
	} else {
		for _, row := range rows {
			height = max(height, astral.Uint64(row.Height))

			err = ch.Send(&OpUpdate{
				Nonce:    row.Nonce,
				ObjectID: row.ObjectID,
				Removed:  astral.Bool(row.Removed),
			})
		}
		height++
	}

	return ch.Send(&height)
}

type OpUpdate struct {
	Nonce    astral.Nonce
	ObjectID *astral.ObjectID
	Removed  astral.Bool
}

var _ astral.Object = &OpUpdate{}

func (s OpUpdate) ObjectType() string { return "mod.user.op_update" }

func (s OpUpdate) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *OpUpdate) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(s).ReadFrom(r)
}

func init() {
	_ = astral.Add(&OpUpdate{})
}
