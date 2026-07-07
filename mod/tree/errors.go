package tree

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/astral"
)

var ErrNodeHasSubnodes = astral.NewError("node has subnodes")
var ErrUnsupported = astral.NewError("unsupported")
var ErrTypeMismatch = errors.New("binding type mismatch")
var ErrAlreadyExists = errors.New("node already exists")
