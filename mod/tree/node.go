package tree

import (
	"errors"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Node defines the interface of a tree node.
type Node interface {
	// Get returns a channel containing the Object held by the node. If follow is false the channel will also
	// be closed. If follow is true, the channel will keep receiving updates until the context is canceled.
	Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error)

	// Set sets the object held by the node.
	Set(ctx *astral.Context, object astral.Object) error

	// Delete deletes the node.
	Delete(ctx *astral.Context) error

	// Sub returns a map of all subnodes, where map keys represent the subnode's name (path segment).
	Sub(ctx *astral.Context) (map[string]Node, error)

	// Create creates and returns a new subnode with the given name.
	Create(ctx *astral.Context, name string) (Node, error)
}

// Query walks the provided path from the provided root and returns the final node. If create
// is true, it will try to create missing nodes along the way.
func Query(ctx *astral.Context, root Node, path string, create bool) (Node, error) {
	seg := strings.Split(
		strings.TrimPrefix(path, "/"),
		"/",
	)

	var pwd []string
	node := root

	for _, s := range seg {
		if len(s) == 0 {
			continue
		}

		subs, err := node.Sub(ctx)
		if err != nil {
			return nil, err
		}

		if n, ok := subs[s]; ok {
			pwd = append(pwd, s)
			node = n
			continue
		}

		if !create {
			return nil, errors.New("node " + s + " not found in /" + strings.Join(pwd, "/"))
		}

		subnode, err := node.Create(ctx, s)
		switch {
		case err == nil:
			node = subnode
		case errors.Is(err, ErrAlreadyExists):
			continue
		default:
			return nil, err
		}
	}

	return node, nil
}

// Get returns the current value of the node
func Get[T astral.Object](ctx *astral.Context, node Node) (T, error) {
	var t T

	object, err := node.Get(ctx, false)
	if err != nil {
		return t, err
	}

	t, ok := (<-object).(T)
	if !ok {
		return t, errors.New("typecast failed")
	}

	return t, nil
}

// Follow returns a typed channel that follows the node values. it ignores values of other types.
func Follow[T astral.Object](ctx *astral.Context, node Node) (<-chan T, *error) {
	var ch = make(chan T, 1)

	values, err := node.Get(ctx, true)
	if err != nil {
		return nil, &err
	}

	// read the initial value to check the type
	first, ok := (<-values).(T)
	if !ok {
		close(ch)
		return nil, &ErrTypeMismatch
	}
	ch <- first

	// subscribe to
	var errPtr = new(error)
	go func() {
		defer close(ch)

		var t T
		var ok bool

		for {
			// read the next value and cast it
			t, ok = (<-values).(T)
			if !ok {
				*errPtr = ErrTypeMismatch
				return
			}

			// send the value to the channel
			select {
			case ch <- t:
			case <-ctx.Done():
			}
		}
	}()

	return ch, errPtr
}
