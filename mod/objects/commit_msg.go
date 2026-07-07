package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type CommitMsg struct{}

func (CommitMsg) ObjectType() string {
	return "mod.objects.commit_msg"
}

func (c CommitMsg) WriteTo(w io.Writer) (n int64, err error) {
	return 0, nil
}

func (c CommitMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return 0, nil
}

func init() {
	_ = astral.Add(&CommitMsg{})
}
