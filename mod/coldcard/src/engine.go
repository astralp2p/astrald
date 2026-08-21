package coldcard

import (
	"encoding/base64"
	"encoding/hex"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/coldcard"
	"github.com/astralp2p/astrald/mod/coldcard/ckcc"
)

type Engine struct {
	mod *Module
}

// NewTextSigner returns a BIP137 signer only for a secp256k1 key whose pubkey
// matches a currently-connected ColdCard; otherwise ErrUnsupported.
func (e *Engine) NewTextSigner(key *crypto.PublicKey, scheme string) (cryptomod.TextSigner, error) {
	switch {
	case scheme != "bip137":
		return nil, cryptomod.ErrUnsupportedScheme
	case key.Type != "secp256k1":
		return nil, cryptomod.ErrUnsupportedKeyType
	}

	pubKeyHex := hex.EncodeToString(key.Key)

	device := e.mod.deviceForPublicKeyHex(pubKeyHex)
	if device != nil {
		return &MessageSigner{dev: device, path: coldcard.BIP44Path}, nil
	}

	return nil, cryptomod.ErrUnsupported
}

type MessageSigner struct {
	dev  *ckcc.Device
	path string
}

// SignText signs on the hardware device; the device returns base64 which is
// decoded into the raw BIP137 signature.
func (m *MessageSigner) SignText(ctx *astral.Context, msg string) (*crypto.Signature, error) {
	sigBase64, err := m.dev.Msg(msg, m.path)
	if err != nil {
		return nil, err
	}

	sig, err := base64.StdEncoding.DecodeString(sigBase64)
	if err != nil {
		return nil, err
	}

	return &crypto.Signature{
		Scheme: "bip137",
		Data:   sig,
	}, nil
}
