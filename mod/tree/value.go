package tree

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
)

// Value wraps an astral.Object type with type-safe access.
type Value[T astral.Object] struct {
	node   Node
	cached T
	queue  *sig.Queue[T]
	// NoInit is currently unused — Bind does not consult this field.
	NoInit bool
	// NoLocal suppresses the fallback that applies a Set locally when the backing node rejects it.
	NoLocal bool
	mu      sync.Mutex
}

var _ astral.Object = &Value[astral.Object]{}

func (value *Value[T]) ObjectType() string {
	var zero T
	v := reflect.ValueOf(&zero)
	v.Elem().Set(reflect.New(v.Elem().Type().Elem()))
	return zero.ObjectType()
}

// Bind binds the value to the node until the context is canceled.
func (value *Value[T]) Bind(ctx *astral.Context, node Node) error {
	value.mu.Lock()
	defer value.mu.Unlock()

	value.node = node
	if value.queue == nil {
		value.queue = &sig.Queue[T]{}
	}

	// follow the node
	updates, err := node.Get(ctx, true)
	if err != nil {
		return err
	}

	// get the initial value
	first := <-updates
	value.update(first, false)

	// subscribe to changes
	go func() {
		for val := range updates {
			value.update(val, true)
		}
		// TODO: try to reconnect on recoverable errors?
	}()

	return nil
}

// BindPath is a convenience function that queries the node and calls Bind.
func (value *Value[T]) BindPath(ctx *astral.Context, node Node, path string, create bool) (err error) {
	node, err = Query(ctx, node, path, create)
	if err != nil {
		return err
	}

	return value.Bind(ctx, node)
}

// Get returns the currently held object
func (value *Value[T]) Get() T {
	value.mu.Lock()
	defer value.mu.Unlock()

	return value.cached
}

// Set updates the value.
func (value *Value[T]) Set(ctx *astral.Context, v T) (err error) {
	value.mu.Lock()
	defer value.mu.Unlock()

	// set the value on the node
	if value.node != nil {
		if any(v) == nil {
			err = value.node.Set(ctx, &astral.Nil{})
		} else {
			err = value.node.Set(ctx, v)
		}
		if err == nil {
			return nil
		}
	}

	// if no local update is allowed, return the error
	if value.NoLocal {
		return err
	}

	// initiate the queue if necessary
	if value.queue == nil {
		value.queue = &sig.Queue[T]{}
	}

	// update the value locally
	value.update(v, true)

	return nil
}

// Clear resets the value to T's zero, persisting it as astral.Nil.
// why: Set cannot clear a pointer T — a typed-nil T slips past its any(v) == nil
// guard and the node would serialize a nil object. Clear sends astral.Nil instead.
func (value *Value[T]) Clear(ctx *astral.Context) (err error) {
	value.mu.Lock()
	defer value.mu.Unlock()

	if value.node != nil {
		if err = value.node.Set(ctx, &astral.Nil{}); err == nil {
			return nil
		}
		if value.NoLocal {
			return err
		}
	}

	if value.queue == nil {
		value.queue = &sig.Queue[T]{}
	}
	value.clear()
	return nil
}

// Follow returns a channel that emits the current value and all updates.
func (value *Value[T]) Follow(ctx *astral.Context) <-chan T {
	value.mu.Lock()
	defer value.mu.Unlock()

	// send the initial value
	var out = make(chan T, 1)
	out <- value.cached

	// initiate the queue if necessary
	if value.queue == nil {
		value.queue = &sig.Queue[T]{}
	}

	// subscribe to updates
	subscribe := sig.Subscribe(ctx, value.queue)
	go func() {
		defer close(out)
		for val := range subscribe {
			if sig.Send(ctx, out, val) != nil {
				return
			}
		}
	}()

	return out
}

// update updates the cached value and pushes it to the queue
func (value *Value[T]) update(val astral.Object, push bool) (ok bool) {
	// convert to the native nil if val is astral.Nil
	if _, isNil := val.(*astral.Nil); isNil || any(val) == nil {
		var zero T
		value.cached = zero
		if push {
			value.queue = value.queue.Push(zero)
		}
		return
	}

	// try to cast the value to cache and push to queue on success
	value.cached, ok = val.(T)
	if ok && push {
		value.queue = value.queue.Push(val.(T))
	}
	return
}

// clear sets the value to nil
func (value *Value[T]) clear() {
	var zero T
	value.cached = zero
	value.queue = value.queue.Push(zero)
}

func (value Value[T]) WriteTo(writer io.Writer) (n int64, err error) {
	if any(value.cached) == nil {
		return 0, errors.New("nil value")
	}
	return value.cached.WriteTo(writer)
}

func (value *Value[T]) ReadFrom(reader io.Reader) (n int64, err error) {
	var obj T
	n, err = obj.ReadFrom(reader)
	if err != nil {
		return
	}

	value.cached = obj
	return
}

func (value Value[T]) MarshalJSON() ([]byte, error) {
	if any(value.cached) == nil {
		return json.Marshal(nil)
	}

	var obj astral.Object = value.cached

	if m, ok := obj.(json.Marshaler); ok {
		return m.MarshalJSON()
	}

	return nil, errors.New("object does not implement json.Marshaler")
}

func (value *Value[T]) UnmarshalJSON(bytes []byte) error {
	var obj T
	err := json.Unmarshal(bytes, &obj)
	if err != nil {
		return err
	}

	value.cached = obj

	return nil
}
