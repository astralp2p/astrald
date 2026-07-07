package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
)

// NilNode returns ErrUnsupported for all operations. Embed it in your node to
// avoid having to explicitly implement unsupported interface methods:
//
//	type MyNode struct {
//		tree.NilNode
//	}
type NilNode struct{}

var _ Node = &NilNode{}

func (NilNode) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	return sig.ArrayToChan([]astral.Object{&astral.Nil{}}), nil
}

func (NilNode) Set(ctx *astral.Context, object astral.Object) error {
	return ErrUnsupported
}

func (NilNode) Delete(ctx *astral.Context) error {
	return ErrUnsupported
}

func (NilNode) Sub(ctx *astral.Context) (map[string]Node, error) {
	return nil, nil
}

func (NilNode) Create(ctx *astral.Context, name string) (Node, error) {
	return nil, ErrUnsupported
}
