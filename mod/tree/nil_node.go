package tree

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// NilNode returns ErrUnsupported for all operations. Embed it in your node to
// avoid having to explicitly implement unsupported interface methods:
//
//	type MyNode struct {
//		tree.NilNode
//	}
type NilNode struct{}

var _ tree.Node = &NilNode{}

func (NilNode) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	return sig.ArrayToChan([]astral.Object{&astral.Nil{}}), nil
}

func (NilNode) Set(ctx *astral.Context, object astral.Object) error {
	return tree.ErrUnsupported
}

func (NilNode) Delete(ctx *astral.Context) error {
	return tree.ErrUnsupported
}

func (NilNode) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	return nil, nil
}

func (NilNode) Create(ctx *astral.Context, name string) (tree.Node, error) {
	return nil, tree.ErrUnsupported
}
