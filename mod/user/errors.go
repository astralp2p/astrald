package user

import "github.com/cryptopunkscc/astral-go/astral"

var ErrInvitationDeclined = astral.NewError("invitation declined")
var ErrRequestDeclined = astral.NewError("request declined")
var ErrNoActiveContract = astral.NewError("no active contract")
var ErrExpelled = astral.NewError("identity expelled from swarm")
