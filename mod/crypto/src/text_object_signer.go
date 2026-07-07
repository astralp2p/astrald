package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"
)

type TextObjectSigner struct {
	mod    *Module
	scheme string
	key    *crypto.PublicKey
}

var _ cryptomod.TextObjectSigner = &TextObjectSigner{}

func (s *TextObjectSigner) SignTextObject(ctx *astral.Context, object crypto.SignableTextObject) (*crypto.Signature, error) {
	signer, err := s.mod.NewTextSigner(s.key, s.scheme)
	if err != nil {
		return nil, err
	}

	return signer.SignText(ctx, s.mod.formatSignableText(object))
}
