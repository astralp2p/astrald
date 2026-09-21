package objects

import (
	"context"
	"fmt"
	"reflect"

	"github.com/astralp2p/astral-go/astral"
)

// Load reads an object from repo, decodes it, and type-asserts it to T.
// Returns ErrObjectTooLarge if the object exceeds MaxObjectSize, or an error if the decoded type is not T.
func Load[T astral.Object](ctx *astral.Context, repo Repository, objectID *astral.ObjectID) (o T, err error) {
	if objectID.Size > uint64(MaxObjectSize) {
		return o, ErrObjectTooLarge
	}

	if ctx == nil {
		ctx = astral.NewContext(context.Background())
	}

	r, err := repo.Read(ctx, objectID, 0, 0)

	if err != nil {
		return o, err
	}
	defer r.Close()

	// why: a partial request carries no size, so only the opened reader knows how large the object is.
	if r.ID().Size > uint64(MaxObjectSize) {
		return o, ErrObjectTooLarge
	}

	var a astral.Object
	var ok bool

	a, _, err = astral.Decode(r, astral.Canonical())
	if err != nil {
		return
	}

	o, ok = a.(T)
	if !ok {
		err = fmt.Errorf("cannot cast %s into %s", reflect.TypeOf(a), reflect.TypeOf(o))
	}

	return
}
