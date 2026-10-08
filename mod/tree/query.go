package tree

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// Query walks path from root and returns the node at its end. With create, a missing segment is
// created, and a segment a concurrent caller created first is descended into.
// why: astral-go tree.Query skips a segment whose Create answers ErrAlreadyExists, so modules that
// bind paths with a shared prefix in parallel at load land their nodes under the wrong parent.
func Query(ctx *astral.Context, root tree.Node, path string, create bool) (tree.Node, error) {
	node := root
	var pwd []string

	for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if len(seg) == 0 {
			continue
		}

		next, err := querySub(ctx, node, seg, create)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, errors.New("node " + seg + " not found in /" + strings.Join(pwd, "/"))
		}

		pwd = append(pwd, seg)
		node = next
	}

	return node, nil
}

// querySub returns the subnode name of node, creating it when create is set. A missing subnode
// without create is nil.
func querySub(ctx *astral.Context, node tree.Node, name string, create bool) (tree.Node, error) {
	subs, err := node.Sub(ctx)
	if err != nil {
		return nil, err
	}
	if sub, ok := subs[name]; ok || !create {
		return sub, nil
	}

	sub, err := node.Create(ctx, name)
	if !errors.Is(err, tree.ErrAlreadyExists) {
		return sub, err
	}

	// note: a concurrent caller created the subnode between Sub and Create.
	subs, err = node.Sub(ctx)
	if err != nil {
		return nil, err
	}
	if sub, ok := subs[name]; ok {
		return sub, nil
	}
	return nil, tree.ErrAlreadyExists
}
