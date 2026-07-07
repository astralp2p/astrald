package apphost

import (
	"errors"
	"math/rand"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
)

func (mod *Module) ListAccessTokens() ([]*apphost.AccessToken, error) {
	rows, err := mod.db.ListAccessTokens()
	if err != nil {
		return nil, err
	}

	return sig.MapSlice(rows, func(a dbAccessToken) (*apphost.AccessToken, error) {
		return &apphost.AccessToken{
			Identity:  a.Identity,
			Token:     astral.String8(a.Token),
			ExpiresAt: astral.Time(a.ExpiresAt),
		}, nil
	})
}

func (mod *Module) CreateAccessToken(identity *astral.Identity, d astral.Duration) (*apphost.AccessToken, error) {
	token, err := mod.db.CreateAccessToken(identity, d)
	if err != nil {
		return nil, err
	}

	return &apphost.AccessToken{
		Identity:  token.Identity,
		Token:     astral.String8(token.Token),
		ExpiresAt: astral.Time(token.ExpiresAt),
	}, nil
}

// AuthenticateToken resolves a bearer token to the identity it was issued for.
// Any lookup or expiry failure is collapsed into a single opaque error to avoid leaking token existence.
func (mod *Module) AuthenticateToken(token string) (*astral.Identity, error) {
	dbToken, err := mod.db.FindAccessToken(token)
	if err != nil || dbToken == nil {
		return nil, errors.New("invalid token")
	}

	return dbToken.Identity, nil
}

func randomString(length int) (s string) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	var name = make([]byte, length)
	for i := 0; i < len(name); i++ {
		name[i] = charset[rand.Intn(len(charset))]
	}
	return string(name[:])
}
