package auth

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

const (
	ModuleName = "auth"
	DBPrefix   = "auth__"
)

// ErrNotAdmitted wraps the refusal of an admitter: IndexContract stored nothing.
var ErrNotAdmitted = errors.New("contract not admitted")

// ContractQueryBuilder builds and executes queries for active signed contracts.
// Filters are cumulative; call Find to execute.
type ContractQueryBuilder interface {
	WithIssuer(*astral.Identity) ContractQueryBuilder
	WithSubject(*astral.Identity) ContractQueryBuilder
	WithAction(...astral.Object) ContractQueryBuilder
	Find(*astral.Context) ([]*auth.SignedContract, error)
}

// SwarmWalk lets Authorize read SwarmState (G3). Until SetSwarm is called,
// Authorize behaves exactly as without it. Once set, at every level of a walk:
//   - root: after the veto, the walk allows the actor when it is the root key
//     or holds a PrivilegedMembership the root key signed, and is not expelled,
//     at any hop and for any action except user.ExpelAction and
//     user.AdminSwarmAction (D7);
//   - Approval link: a contract with an ApprovalID holds only when that
//     Approval is stored, its request is approved, the request was filed (a
//     ContractRequest) or carried in (a JoinRequest) by the contract's issuer,
//     names its subject and covers its permits (a JoinRequest: at the tier the
//     contract grants), the approver is the root key or its tier may answer
//     that request (user.MayAnswer), and neither the issuer, the subject nor
//     the approver is expelled or vetoed. The walk then continues as the
//     approver instead of the issuer. A contract with no ApprovalID is followed
//     to its issuer as before.
//
// root returns the root key: the user this node is claimed by, or the node
// itself while unclaimed. Module embeds it; the user module calls SetSwarm
// once in LoadDependencies.
type SwarmWalk interface {
	SetSwarm(view usermod.SwarmView, state nodesmod.SwarmState, root func() *astral.Identity)
}

type Module interface {
	SwarmWalk
	// Authorize checks whether the action is permitted.
	Authorize(ctx *astral.Context, action auth.ActionObject) bool

	// Add registers typed handlers; the action type is inferred from each handler.
	Add(handlers ...TypedHandler)

	// VerifyContract verifies both signatures on a signed contract.
	VerifyContract(sc *auth.SignedContract) error

	// VerifyIssuer verifies only the issuer signature on a signed contract.
	VerifyIssuer(sc *auth.SignedContract) error

	// VerifySubject verifies only the subject signature on a signed contract.
	VerifySubject(sc *auth.SignedContract) error

	// SignIssuer signs the contract with the issuer's private key if needed.
	SignIssuer(ctx *astral.Context, contract *auth.SignedContract) (*crypto.Signature, error)

	// SignSubject signs the contract with the subject's private key if needed.
	SignSubject(ctx *astral.Context, contract *auth.SignedContract) (*crypto.Signature, error)

	// SignContract signs the contract with both issuer and subject private keys if needed.
	SignContract(ctx *astral.Context, contract *auth.SignedContract) error

	// IsNodeClaim reports whether c is the contract that claims this unclaimed
	// node, asked for by caller: the one contract auth.sign_contract signs
	// without the signer rule.
	IsNodeClaim(ctx *astral.Context, caller *astral.Identity, c *auth.Contract) bool

	// IndexContract verifies and adds a signed contract to the auth index.
	// A contract an admitter refuses is not added, and the error wraps
	// ErrNotAdmitted.
	IndexContract(ctx *astral.Context, contract *auth.SignedContract) error

	// AddAdmitter registers fn to run on each contract IndexContract would add
	// to the index. A non-nil answer refuses the contract. fn runs outside the
	// index lock and never for a contract the index already holds.
	AddAdmitter(fn func(*auth.SignedContract) error)

	// AddVeto registers fn to run first at every level of an authorization
	// walk, with the identity that level names: the caller at the first level,
	// then each contract issuer the walk reaches. A true answer refuses the
	// level: no handler, contract or external authority is asked for that
	// identity, though the walk goes on to the caller's other contracts. It
	// cuts an identity off whatever permits it still holds. fn runs often and
	// must answer from memory.
	AddVeto(fn func(actor *astral.Identity) bool)

	// OnContractIndexed registers fn to run after each contract the index did
	// not hold before is added to it. fn runs outside the index lock.
	OnContractIndexed(fn func(*auth.SignedContract))

	// SignedContracts returns a query builder for finding active signed contracts.
	SignedContracts() ContractQueryBuilder

	// WithdrawContracts takes back, on this node alone, every unexpired contract
	// issued by issuer that carries a permit for action, and returns how many it
	// took. The contract stays in the object store but no authorization, query
	// or index reads it again. A copy on another node is untouched.
	WithdrawContracts(issuer *astral.Identity, action astral.Object) (int, error)

	// OnContractWithdrawn registers fn to run after each WithdrawContracts call
	// that took back at least one contract, with the issuer it named. fn runs
	// synchronously inside the call, once per call, and must not block.
	OnContractWithdrawn(fn func(issuer *astral.Identity))

	// SetAuthorizer persists an external authorizer assignment, replacing the
	// one with the same action and actor, and applies it to the next
	// authorization. A failure leaves both the tree and the registry unchanged.
	SetAuthorizer(ctx *astral.Context, assignment *auth.AuthorizerAssignment) error

	// NewAuthorizeAsk builds an asker for an endpoint, in the forms an external
	// authorizer takes: http://, https:// or astral://<identity-or-alias>:<op>.
	// Every question, its send and its receive, is bounded by timeout; a
	// timeout of 0 or less is the default of an external authorizer. The asker
	// remembers no answer.
	NewAuthorizeAsk(endpoint string, timeout time.Duration) (auth.AuthorizeAsk, error)
}
