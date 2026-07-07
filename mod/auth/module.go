package auth

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

const (
	ModuleName         = "auth"
	DBPrefix           = "auth__"
	MethodIndex        = "auth.index"
	MethodSignContract = "auth.sign_contract"
)

// ContractQueryBuilder builds and executes queries for active signed contracts.
// Filters are cumulative; call Find to execute.
type ContractQueryBuilder interface {
	WithIssuer(*astral.Identity) ContractQueryBuilder
	WithSubject(*astral.Identity) ContractQueryBuilder
	WithAction(...astral.Object) ContractQueryBuilder
	Find(*astral.Context) ([]*SignedContract, error)
}

type Module interface {
	// Authorize checks whether the action is permitted.
	Authorize(ctx *astral.Context, action ActionObject) bool

	// Add registers typed handlers; the action type is inferred from each handler.
	Add(handlers ...TypedHandler)

	// VerifyContract verifies both signatures on a signed contract.
	VerifyContract(sc *SignedContract) error

	// VerifyIssuer verifies only the issuer signature on a signed contract.
	VerifyIssuer(sc *SignedContract) error

	// VerifySubject verifies only the subject signature on a signed contract.
	VerifySubject(sc *SignedContract) error

	// SignIssuer signs the contract with the issuer's private key if needed.
	SignIssuer(ctx *astral.Context, contract *SignedContract) (*crypto.Signature, error)

	// SignSubject signs the contract with the subject's private key if needed.
	SignSubject(ctx *astral.Context, contract *SignedContract) (*crypto.Signature, error)

	// SignContract signs the contract with both issuer and subject private keys if needed.
	SignContract(ctx *astral.Context, contract *SignedContract) error

	// IndexContract verifies and adds a signed contract to the auth index.
	IndexContract(ctx *astral.Context, contract *SignedContract) error

	// SignedContracts returns a query builder for finding active signed contracts.
	SignedContracts() ContractQueryBuilder
}
