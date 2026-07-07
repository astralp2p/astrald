package indexing

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &IndexMsg{}

type IndexMsg struct {
	Repo     astral.String8
	Version  astral.Uint64
	ObjectID *astral.ObjectID
}

func (IndexMsg) ObjectType() string { return "indexing.index" }

func (i IndexMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&i).WriteTo(w)
}

func (i *IndexMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(i).ReadFrom(r)
}

var _ astral.Object = &UnindexMsg{}

type UnindexMsg struct {
	Repo     astral.String8
	Version  astral.Uint64
	ObjectID *astral.ObjectID
}

func (UnindexMsg) ObjectType() string { return "indexing.unindex" }

func (u UnindexMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&u).WriteTo(w)
}

func (u *UnindexMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(u).ReadFrom(r)
}

var _ astral.Object = &ChangeAckMsg{}

type ChangeAckMsg struct {
	Repo    astral.String8
	Version astral.Uint64
}

func (ChangeAckMsg) ObjectType() string { return "indexing.ack" }

func (a ChangeAckMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *ChangeAckMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() {
	_ = astral.Add(&IndexMsg{})
	_ = astral.Add(&UnindexMsg{})
	_ = astral.Add(&ChangeAckMsg{})
}
