package crypto

import (
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

type ObjectSigner struct {
	mod    *Module
	scheme string
	key    *crypto.PublicKey
}

var _ cryptomod.ObjectSigner = &ObjectSigner{}

func (s *ObjectSigner) SignObject(ctx *astral.Context, object crypto.SignableObject) (*crypto.Signature, error) {
	signer, err := s.mod.NewHashSigner(s.key, s.scheme)
	if err != nil {
		return nil, err
	}

	return signer.SignHash(ctx, object.SignableHash())
}
