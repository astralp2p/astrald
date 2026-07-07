package tree

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

type Node struct {
	client *Client
	path   []string
}

var _ tree.Node = &Node{}

func (node *Node) Name() string {
	if len(node.path) == 0 {
		return ""
	}
	return node.path[len(node.path)-1]
}

// Get fetches the node's current value; when follow is true the returned channel
// stays open and emits successive updates until ctx is cancelled or the server closes.
func (node *Node) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	ch, err := node.client.queryCh(ctx, tree.MethodGet, query.Args{
		"path":   node.Path(),
		"follow": follow,
	})
	if err != nil {
		return nil, err
	}

	// get the initial value
	obj, err := ch.Receive()
	if err != nil {
		return nil, err
	}

	// make the output channel
	var out = make(chan astral.Object, 1)
	out <- obj

	if !follow {
		ch.Close()
		close(out)
		return out, nil
	}

	go func() {
		defer ch.Close()
		defer close(out)
		ch.Switch(
			channel.BreakOnEOS,
			func(obj astral.Object) {
				select {
				case <-ctx.Done():
					return
				case out <- obj:
				}
			},
		)
	}()

	return out, nil
}

func (node *Node) Set(ctx *astral.Context, object astral.Object) error {
	ch, err := node.client.queryCh(ctx, tree.MethodSet, query.Args{
		"path": node.Path(),
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	err = ch.Send(object)
	if err != nil {
		return err
	}

	msg, err := ch.Receive()
	switch msg := msg.(type) {
	case *astral.Ack:
		return nil
	case nil:
		return err
	case *astral.ErrorMessage:
		return msg
	default:
		return astral.NewErrUnexpectedObject(msg)
	}
}

func (node *Node) Delete(ctx *astral.Context) error {
	ch, err := node.client.queryCh(ctx, tree.MethodDelete, query.Args{
		"path": node.Path(),
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	msg, err := ch.Receive()
	switch msg := msg.(type) {
	case *astral.Ack:
		return nil
	case nil:
		return err
	case *astral.ErrorMessage:
		return msg
	default:
		return astral.NewErrUnexpectedObject(msg)
	}
}

// Sub returns the immediate children of the node keyed by their names.
func (node *Node) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	var sub = make(map[string]tree.Node)

	ch, err := node.client.queryCh(ctx, tree.MethodList, query.Args{
		"path": node.Path(),
	})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	err = ch.Switch(
		func(msg *astral.String8) {
			sub[string(*msg)] = &Node{client: node.client, path: append(node.path, string(*msg))}
		},
		channel.BreakOnEOS,
		channel.PassErrors,
	)

	if err != nil {
		return nil, err
	}

	return sub, nil
}

// Create opens a child node at name, creating it server-side without setting a value.
func (node *Node) Create(ctx *astral.Context, name string) (tree.Node, error) {
	newPath := "/" + strings.Join(append(node.path, name), "/")

	// calling set without sending any value will still create the node
	ch, err := node.client.queryCh(ctx, tree.MethodSet, query.Args{
		"path": newPath,
	})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	return &Node{client: node.client, path: append(node.path, name)}, nil
}

func (node *Node) Path() string {
	return "/" + strings.Join(node.path, "/")
}
