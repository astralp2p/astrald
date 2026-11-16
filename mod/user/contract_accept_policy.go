package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// ContractAcceptPolicy decides whether user.accept_contract activates a fully
// signed node contract that passed validation. Allow false declines. An error
// is a question that could not be decided, and the node declines.
type ContractAcceptPolicy func(ctx *astral.Context, req *user.ContractAcceptRequest) (*user.ContractAcceptDecision, error)

// AcceptAllContracts is the accept_all contract accept policy: every contract
// that passed validation is accepted.
func AcceptAllContracts(_ *astral.Context, _ *user.ContractAcceptRequest) (*user.ContractAcceptDecision, error) {
	return &user.ContractAcceptDecision{Allow: true}, nil
}

// RejectAllContracts is the reject_all contract accept policy and the code
// default: every contract is declined.
func RejectAllContracts(_ *astral.Context, _ *user.ContractAcceptRequest) (*user.ContractAcceptDecision, error) {
	return &user.ContractAcceptDecision{}, nil
}
