package apphost

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "apphost"
const DBPrefix = "apphost__"

// DefaultTokenDuration is the lifetime of an access token issued without a
// lifetime named.
const DefaultTokenDuration = astral.Duration(time.Hour * 24 * 365) // 1 year

// Extra keys apphost sets on an inbound guest query so ops can apply their own
// authorization. Each is set only when true; absence carries the negative.
const (
	ExtraOriginWeb  = "origin-web"  // browser Origin header; set for WebSocket guests
	ExtraAnonymous  = "anonymous"   // set when the guest session presented no token
	ExtraHTTPRemote = "http-remote" // set on an HTTP query from a non-loopback TCP client
)

// Module is the public API surface of the apphost module.
type Module interface {
	CreateAccessToken(*astral.Identity, astral.Duration) (*apphost.AccessToken, error)
	AuthenticateToken(string) (*astral.Identity, error)
	DeleteAccessToken(string) error

	// DeleteAccessTokens removes every access token issued for identity,
	// expired ones included. An identity holding none is not an error.
	DeleteAccessTokens(identity *astral.Identity) error

	// ProvisionApp mints an App identity as apphost.register does, holding a
	// node→app contract of contractPermits: a key pair, a signed app contract,
	// that contract and an access token.
	ProvisionApp(ctx *astral.Context, contractPermits []*auth.Permit) (*apphost.AccessToken, error)

	// RequestContract files a ContractRequest asking permits for app, as
	// apphost.request_contract does, and returns its entry ID. The host signs
	// the app's contract once it co-signed an offer for it. It answers
	// ErrContractRequestsOff on a node without SwarmState.
	RequestContract(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*astral.ObjectID, error)

	// IsSwarmApp reports whether app is an app hosted on a node of the local
	// swarm, this node included: the issuer of an active relay-for contract
	// whose subject is a swarm node. A node without a user has no swarm apps.
	IsSwarmApp(ctx *astral.Context, app *astral.Identity) bool
}

var ErrMissingAppIdentity = errors.New("missing app identity")
var ErrMissingObjectID = errors.New("missing object id")
var ErrInvalidIdentity = errors.New("invalid identity")
var ErrInvalidPermit = errors.New("invalid permit")

// Errors apphost.request_contract answers. An app that gets
// ErrContractDeclined or ErrContractRequestExpired files a new request by
// calling the op again.
var (
	ErrContractDeclined           = errors.New("contract request declined")
	ErrContractRequestExpired     = errors.New("contract request expired")
	ErrContractRequestRateLimited = errors.New("too many contract requests")
	ErrContractRequestsOff        = errors.New("contract requests not available")
)
