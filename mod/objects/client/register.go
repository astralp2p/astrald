package objects

import (
	"fmt"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// Register pushes a runtime *astral.Blueprint (struct or alias kind) to the remote node and
// returns its content-addressed ObjectID.
func (client *Client) Register(ctx *astral.Context, o astral.Object) (id *astral.ObjectID, err error) {
	ch, err := client.queryCh(ctx, objects.MethodRegisterBlueprint, nil)
	if err != nil {
		return
	}
	defer ch.Close()

	if err = ch.Send(o); err != nil {
		return
	}

	err = ch.Switch(channel.Expect(&id), channel.PassErrors)
	if err != nil && strings.HasPrefix(err.Error(), astral.ErrAlreadyRegistered.Error()) {
		return nil, fmt.Errorf("%w: %s", astral.ErrAlreadyRegistered, typeNameOf(o))
	}
	return
}

func Register(ctx *astral.Context, o astral.Object) (*astral.ObjectID, error) {
	return Default().Register(ctx, o)
}

// typeNameOf returns the blueprint-side type name of a *astral.Blueprint for use in error
// messages. Falls back to ObjectType for any other input.
func typeNameOf(o astral.Object) string {
	if v, ok := o.(*astral.Blueprint); ok {
		return v.Type.String()
	}
	return o.ObjectType()
}
