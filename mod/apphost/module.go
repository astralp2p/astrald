package apphost

import (
	"errors"
	"github.com/astralp2p/astral-go/api/apphost"

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
	LocalApps() ([]*apphost.App, error)
}

var ErrMissingAppIdentity = errors.New("missing app identity")
var ErrMissingObjectID = errors.New("missing object id")
