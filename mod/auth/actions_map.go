package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"reflect"

	"github.com/astralp2p/astral-go/astral"
)

// Handler authorizes a typed action object.
// The action object carries the actor identity — no separate identity argument.
type Handler interface {
	Authorize(*astral.Context, auth.ActionObject) bool
}

// TypedHandler is a Handler that also knows its action ObjectType string.
type TypedHandler interface {
	Handler
	ActionType() string
}

// Func is a generic adapter implementing TypedHandler for a specific action type T.
// It type-asserts the incoming action object to T before dispatching.
// T must be an ActionObject (i.e. a concrete action type embedding auth.Action).
type Func[T auth.ActionObject] func(*astral.Context, T) bool

func (f Func[T]) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	if t, ok := action.(T); ok {
		return f(ctx, t)
	}
	return false
}

func (Func[T]) ActionType() string {
	t := reflect.TypeOf((*T)(nil)).Elem()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return reflect.New(t).Interface().(auth.ActionObject).ObjectType()
}

// NodeLocal marks a handler whose authority answers for this node alone, so
// that Authorize consults it for the caller and never for a contract's issuer.
//
// why: a node-local record is not portable evidence — no other node reads it —
// so the identity holding one has nothing to hand on. Reaching such a handler
// from a chain link would let its holder extend the record a hop by issuing a
// contract for the same action.
func NodeLocal(h TypedHandler) TypedHandler {
	return nodeLocal{h}
}

type nodeLocal struct{ TypedHandler }

func (nodeLocal) NodeLocal() {}
