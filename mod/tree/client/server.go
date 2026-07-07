package tree

import (
	"encoding"
	"sort"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

// NodeOps is a set of tree node operations that can be used with lib/routing
type NodeOps struct {
	Node tree.Node
}

func NewNodeOps(node tree.Node) *NodeOps {
	return &NodeOps{Node: node}
}

type GetArgs struct {
	Path   string `query:"required"`
	Follow bool
	In     string
	Out    string
}

// Get serves a get query; when Follow is set it streams updates until ctx is done
// or the client closes the channel, detected by a background Receive on the accepted channel.
func (ops *NodeOps) Get(ctx *astral.Context, q *routing.IncomingQuery, args GetArgs) (err error) {
	ctx, cancel := ctx.WithCancel()
	defer cancel()

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	node, err := tree.Query(ctx, ops.Node, args.Path, false)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// Cancel the surrounding context once the caller's side of the channel
	// goes away (EOF / WS close). Receive blocks indefinitely on an idle
	// connection, so a healthy follow stream isn't disrupted.
	go func() {
		if _, err := ch.Receive(); err != nil {
			cancel()
		}
	}()

	val, err := node.Get(ctx, args.Follow)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for object := range val {
		ch.Send(object)
	}

	return ch.Send(&astral.EOS{})
}

type SetArgs struct {
	Path  string `query:"required"`
	Type  string // inferred from the current value if empty
	Value string // batch-mode if empty
	In    string
	Out   string
}

// Set handles a set query; routes to single-value mode when args.Value is non-empty,
// otherwise enters batch mode and reads objects from the channel until EOS.
func (ops *NodeOps) Set(ctx *astral.Context, q *routing.IncomingQuery, args SetArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Value != "" {
		return ops.setSingle(ctx, ch, args)
	}
	return ops.setBatch(ctx, ch, args)
}

func (ops *NodeOps) setSingle(ctx *astral.Context, ch *channel.Channel, args SetArgs) error {
	// Create the node only when an explicit type was given. Inference needs
	// an existing value to read the type from, so we don't auto-create.
	createIfMissing := args.Type != ""
	node, err := tree.Query(ctx, ops.Node, args.Path, createIfMissing)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	typeName := args.Type
	if typeName == "" {
		cur, err := node.Get(ctx, false)
		if err != nil {
			return ch.Send(astral.NewError("cannot infer type: " + err.Error()))
		}
		curVal := <-cur
		if _, isNil := curVal.(*astral.Nil); curVal == nil || isNil {
			return ch.Send(astral.NewError("cannot infer type: node has no current value"))
		}
		typeName = curVal.ObjectType()
	}

	obj := astral.New(typeName)
	if obj == nil {
		return ch.Send(astral.NewError("unknown object type: " + typeName))
	}

	u, ok := obj.(encoding.TextUnmarshaler)
	if !ok {
		return ch.Send(astral.NewError("type does not support text encoding: " + typeName))
	}

	if err := u.UnmarshalText([]byte(args.Value)); err != nil {
		return ch.Send(astral.NewError("parse value: " + err.Error()))
	}

	if err := node.Set(ctx, obj); err != nil {
		return ch.Send(astral.Err(err))
	}
	return ch.Send(&astral.Ack{})
}

func (ops *NodeOps) setBatch(ctx *astral.Context, ch *channel.Channel, args SetArgs) error {
	node, err := tree.Query(ctx, ops.Node, args.Path, true)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Switch(
		func(object astral.Object) error {
			err := node.Set(ctx, object)
			if err != nil {
				return ch.Send(astral.Err(err))
			}
			return ch.Send(&astral.Ack{})
		},
		channel.BreakOnEOS,
		channel.WithContext(ctx),
	)
}

type DeleteArgs struct {
	Path      string `query:"required"`
	Recursive bool
	In        string
	Out       string
}

// Delete handles a delete query; when Recursive is set, descends the subtree
// depth-first and deletes from the leaves up before removing the target node.
func (ops *NodeOps) Delete(ctx *astral.Context, q *routing.IncomingQuery, args DeleteArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	node, err := tree.Query(ctx, ops.Node, args.Path, false)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if args.Recursive {
		err = deleteRecursive(ctx, node)
	} else {
		err = node.Delete(ctx)
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// deleteRecursive walks the subtree depth-first via the public Node API and
// deletes from the leaves up. Remote-mounted subtrees are recursed through —
// each Sub()/Delete() call there hops to the remote node.
func deleteRecursive(ctx *astral.Context, n tree.Node) error {
	subs, err := n.Sub(ctx)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if err := deleteRecursive(ctx, sub); err != nil {
			return err
		}
	}
	return n.Delete(ctx)
}

type ListArgs struct {
	Path string
	In   string
	Out  string
}

func (ops *NodeOps) List(ctx *astral.Context, query *routing.IncomingQuery, args ListArgs) (err error) {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var path = "/"
	if len(args.Path) > 0 {
		path = args.Path
	}

	node, err := tree.Query(ctx, ops.Node, path, false)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	subs, err := node.Sub(ctx)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// Map iteration order is non-deterministic; sort so UIs don't reshuffle
	// on every refresh.
	names := make([]string, 0, len(subs))
	for name := range subs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := ch.Send((*astral.String8)(&name)); err != nil {
			// The send failed because the channel is broken — sending an
			// ErrorMessage on the same channel can only fail too. Just exit.
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
