package apphost

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
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

	// Grant adds a permit for identity on this node beside the grants it already
	// holds; an identical permit refreshes its expiry. A nil expiresAt grants
	// until revoked. The permit is recorded, not signed: it authorizes here and
	// travels nowhere.
	Grant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error

	// Revoke withdraws every grant identity holds for action, named by its
	// object type.
	Revoke(identity *astral.Identity, action string) error

	// RevokeGrant withdraws one grant by its ID. An ID of another identity's
	// grant is not found.
	RevokeGrant(identity *astral.Identity, id uint64) error

	// Grants returns every permit granted to identity, expired included.
	Grants(identity *astral.Identity) ([]*auth.Permit, error)

	// ProvisionApp mints an App identity as apphost.register does, without
	// asking the app register policy: a key pair, a signed app contract
	// holding contractPermits, node-local grants of grantPermits beside the
	// grants every app holds, and an access token.
	ProvisionApp(ctx *astral.Context, grantPermits, contractPermits []*auth.Permit) (*apphost.AccessToken, error)

	// SetPolicyDelegate stores d as the delegate of the named apphost policy
	// (app_register or grant_request), or clears it when d is nil. Another
	// name is an error.
	SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error

	// IsSwarmApp reports whether app is an app hosted on a node of the local
	// swarm, this node included: the issuer of an active relay-for contract
	// whose subject is a swarm node. A node without a user has no swarm apps.
	IsSwarmApp(ctx *astral.Context, app *astral.Identity) bool
}

var ErrMissingAppIdentity = errors.New("missing app identity")
var ErrMissingObjectID = errors.New("missing object id")
var ErrInvalidIdentity = errors.New("invalid identity")
var ErrInvalidPermit = errors.New("invalid permit")
