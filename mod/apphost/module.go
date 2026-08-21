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

// Extra keys apphost sets on an inbound guest query so ops can apply their own
// authorization. Both are set only when true; absence carries the negative.
const (
	ExtraOriginWeb = "origin-web" // browser Origin header; set for WebSocket guests
	ExtraAnonymous = "anonymous"  // set when the guest session presented no token
)

// Module is the public API surface of the apphost module.
type Module interface {
	CreateAccessToken(*astral.Identity, astral.Duration) (*apphost.AccessToken, error)
	AuthenticateToken(string) (*astral.Identity, error)
	DeleteAccessToken(string) error

	// Grant records a permit for identity on this node, replacing whatever it
	// held for the same action. A nil expiresAt grants until revoked. The permit
	// is recorded, not signed: it authorizes here and travels nowhere.
	Grant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error

	// Revoke withdraws identity's grant for action, named by its object type.
	Revoke(identity *astral.Identity, action string) error

	// Grants returns every permit granted to identity, expired included.
	Grants(identity *astral.Identity) ([]*auth.Permit, error)
}

var ErrMissingAppIdentity = errors.New("missing app identity")
var ErrMissingObjectID = errors.New("missing object id")
var ErrInvalidIdentity = errors.New("invalid identity")
var ErrInvalidPermit = errors.New("invalid permit")
