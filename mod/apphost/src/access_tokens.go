package apphost

import (
	"crypto/rand"
	"errors"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
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

// DeleteAccessToken removes an access token so it no longer authenticates.
// Returns gorm.ErrRecordNotFound when the token does not exist.
func (mod *Module) DeleteAccessToken(token string) error {
	return mod.db.DeleteAccessToken(token)
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

// randomString returns length characters drawn uniformly from charset,
// sourced from crypto/rand.
func randomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"

	// why: 63 does not divide 256, so folding every byte into the charset would
	// make the first four characters likelier than the rest. Bytes at or above
	// the last whole multiple of 63 are discarded instead.
	const limit = 256 - 256%len(charset)

	var (
		out = make([]byte, 0, length)
		buf = make([]byte, length)
	)

	for len(out) < length {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}

		for _, b := range buf {
			if len(out) == length {
				break
			}
			if int(b) < limit {
				out = append(out, charset[int(b)%len(charset)])
			}
		}
	}

	return string(out), nil
}
