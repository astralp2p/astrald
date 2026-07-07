package src

import (
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/bip137sig"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type Deps struct {
	Crypto cryptomod.Module
}

type Module struct {
	Deps
	node   astral.Node
	log    *log.Logger
	assets assets.Assets
	router routing.OpRouter
}

var _ bip137sig.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) CryptoEngine() cryptomod.Engine {
	return &Engine{mod: mod}
}

func (mod *Module) String() string {
	return bip137sig.ModuleName
}

// DeriveKey derives a secp256k1 private key from seed along the BIP-32 path on mainnet.
func (mod *Module) DeriveKey(seed bip137sig.Seed, path string) (privateKey crypto.PrivateKey, err error) {
	derivationPath, err := bip137sig.ParseDerivationPath(path)
	if err != nil {
		return privateKey, err
	}

	key, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
	if err != nil {
		return privateKey, err
	}

	for _, idx := range derivationPath {
		key, err = key.Derive(idx)
		if err != nil {
			return privateKey, err
		}
	}

	ecpPrivateKey, err := key.ECPrivKey()
	if err != nil {
		return
	}

	return crypto.PrivateKey{
		Type: secp256k1.KeyType,
		Key:  ecpPrivateKey.Serialize(),
	}, nil
}

// GenerateSeed builds a seed from fresh default-strength entropy with an empty passphrase.
func (mod *Module) GenerateSeed() (seed bip137sig.Seed, err error) {
	entropy, err := bip137sig.NewEntropy(bip137sig.DefaultEntropyBits)
	if err != nil {
		return seed, err
	}

	words, err := bip137sig.EntropyToMnemonic(entropy)
	if err != nil {
		return seed, err
	}

	return bip137sig.MnemonicToSeed(words, "")
}
