package tree

import (
	"errors"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Node implements a tree.Node that stores objects in the database.
type Node struct {
	mod   *Module
	id    int
	name  string
	value astral.Object
}

var _ tree.Node = &Node{}

func (node *Node) Name() string {
	return node.name
}

// Get returns a channel that yields the current value; if follow is true the channel stays open
// and delivers each subsequent value until ctx is canceled. The root node cannot hold a value.
func (node *Node) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	if node.name == "" {
		return nil, errors.New("root node cannot hold a value")
	}

	object, err := node.mod.db.getNodeValue(node.id, true)
	if err != nil {
		return nil, err
	}
	if object == nil {
		object = &astral.Nil{}
	}

	ch := make(chan astral.Object, 1)
	ch <- object

	if !follow {
		close(ch)
		return ch, nil
	}

	values := node.mod.subscribeNodeValue(ctx, node.id)

	go func() {
		defer close(ch)
		for value := range values {
			select {
			case <-ctx.Done():
				return
			case ch <- value:
			}
		}
	}()

	return ch, nil
}

// Set persists object to the DB and then notifies all active Get(follow=true) subscribers.
func (node *Node) Set(ctx *astral.Context, object astral.Object) error {
	if node.name == "" {
		return errors.New("root node cannot hold a value")
	}

	if object == nil {
		object = &astral.Nil{}
	}

	defer node.mod.pushNodeValue(node.id, object)

	return node.mod.db.setNodeValue(node.id, object)
}

func (node *Node) Delete(ctx *astral.Context) error {
	return node.mod.deleteNode(node.id)
}

func (node *Node) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	row, err := node.mod.db.getSubNodes(node.id)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]tree.Node, len(row))
	for _, row := range row {
		nodes[row.Name] = &Node{
			mod:  node.mod,
			id:   row.ID,
			name: row.Name,
		}
	}

	return nodes, nil
}

func (node *Node) Create(ctx *astral.Context, name string) (tree.Node, error) {
	if len(name) == 0 {
		return nil, errors.New("name cannot be empty")
	}

	row, err := node.mod.db.createNode(node.id, name)
	switch {
	case err == nil:
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		return nil, tree.ErrAlreadyExists
	default:
		return nil, err
	}

	return &Node{
		mod:  node.mod,
		id:   row.ID,
		name: row.Name,
	}, nil
}
