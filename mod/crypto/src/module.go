package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type Deps struct {
	Dir     dir.Module
	Objects objects.Module
}

type Module struct {
	Deps
	config  Config
	node    astral.Node
	log     *log.Logger
	nodeKey *crypto.PrivateKey
	assets  assets.Assets
	db      *DB
	router  routing.OpRouter
	ctx     *astral.Context

	engines sig.Set[cryptomod.Engine]
}

var _ cryptomod.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx

	for _, repoName := range mod.config.Repos {
		repo := mod.Objects.GetRepository(repoName)
		if repo == nil {
			mod.log.Logv(1, "cannot index %v: repository not found", repoName)
		}

		go func() {
			err := mod.indexRepo(ctx, repo)
			if err != nil {
				mod.log.Logv(1, "indexing %v disabled: %v", repo.Label(), err)
			}
		}()
	}

	<-ctx.Done()
	return nil
}

func (mod *Module) PrivateKeyID(key *crypto.PublicKey) (*astral.ObjectID, error) {
	keyText, err := key.MarshalText()
	if err != nil {
		return nil, err
	}

	row, err := mod.db.findPrivateKeyByPublicKey(string(keyText))
	if err != nil {
		return nil, err
	}

	return row.KeyID, nil
}

func (mod *Module) PrivateKey(ctx *astral.Context, key *crypto.PublicKey) (*crypto.PrivateKey, error) {
	keyText, err := key.MarshalText()
	if err != nil {
		return nil, err
	}

	row, err := mod.db.findPrivateKeyByPublicKey(string(keyText))
	if err != nil {
		return nil, err
	}

	object, err := mod.Objects.Load(ctx, mod.Objects.ReadDefault(), row.KeyID)
	switch object := object.(type) {
	case nil:
		return nil, err

	case *crypto.PrivateKey:
		return object, nil

	default:
		return nil, astral.NewErrUnexpectedObject(object)
	}
}

func (mod *Module) DerivePublicKey(ctx *astral.Context, key *crypto.PrivateKey) (*crypto.PublicKey, error) {
	return dispatchResult[cryptomod.PublicKeyDeriver, *crypto.PublicKey](
		mod.engines.Clone(),
		func(d cryptomod.PublicKeyDeriver) (*crypto.PublicKey, error) {
			return d.DerivePublicKey(ctx, key)
		},
	)
}

func (mod *Module) NewHashSigner(key *crypto.PublicKey, scheme string) (cryptomod.HashSigner, error) {
	return dispatchResult[cryptomod.HashSignerProvider, cryptomod.HashSigner](
		mod.engines.Clone(),
		func(p cryptomod.HashSignerProvider) (cryptomod.HashSigner, error) {
			return p.NewHashSigner(key, scheme)
		},
	)
}

// NodeSigner returns a signer for the node's own secp256k1 key.
// Panics if no engine can provide one.
func (mod *Module) NodeSigner() cryptomod.HashSigner {
	signer, err := mod.NewHashSigner(&crypto.PublicKey{
		Type: "secp256k1",
		Key:  mod.node.Identity().PublicKey().SerializeCompressed(),
	}, crypto.SchemeASN1)
	if err != nil {
		panic(err)
	}

	return signer
}

func (mod *Module) VerifyHashSignature(key *crypto.PublicKey, sig *crypto.Signature, hash []byte) error {
	switch {
	case key == nil:
		return errors.New("public key is nil")
	case len(key.Key) == 0:
		return errors.New("public key data is empty")
	case len(key.Type) == 0:
		return errors.New("public key type is empty")
	case sig == nil:
		return errors.New("signature is nil")
	case len(sig.Data) == 0:
		return errors.New("signature data is empty")
	case len(sig.Scheme) == 0:
		return errors.New("signature scheme is empty")
	case len(hash) == 0:
		return errors.New("hash is empty")
	}

	return dispatchVerify[cryptomod.HashVerifier](
		mod.engines.Clone(),
		func(v cryptomod.HashVerifier) error {
			return v.VerifyHashSignature(key, sig, hash)
		},
	)
}

func (mod *Module) NewTextSigner(key *crypto.PublicKey, scheme string) (cryptomod.TextSigner, error) {
	return dispatchResult[cryptomod.TextSignerProvider, cryptomod.TextSigner](
		mod.engines.Clone(),
		func(p cryptomod.TextSignerProvider) (cryptomod.TextSigner, error) {
			return p.NewTextSigner(key, scheme)
		},
	)
}

func (mod *Module) VerifyTextSignature(key *crypto.PublicKey, sig *crypto.Signature, msg string) error {
	switch {
	case key == nil:
		return errors.New("public key is nil")
	case len(key.Key) == 0:
		return errors.New("public key data is empty")
	case len(key.Type) == 0:
		return errors.New("public key type is empty")
	case sig == nil:
		return errors.New("signature is nil")
	case len(sig.Data) == 0:
		return errors.New("signature data is empty")
	case len(sig.Scheme) == 0:
		return errors.New("signature scheme is empty")
	case len(msg) == 0:
		return errors.New("message is empty")
	}

	return dispatchVerify[cryptomod.TextVerifier](
		mod.engines.Clone(),
		func(v cryptomod.TextVerifier) error {
			return v.VerifyTextSignature(key, sig, msg)
		},
	)
}

func (mod *Module) ObjectSigner(key *crypto.PublicKey) (cryptomod.ObjectSigner, error) {
	return &ObjectSigner{
		mod:    mod,
		scheme: crypto.SchemeASN1,
		key:    key,
	}, nil
}

func (mod *Module) VerifyObjectSignature(key *crypto.PublicKey, signature *crypto.Signature, object crypto.SignableObject) error {
	return mod.VerifyHashSignature(key, signature, object.SignableHash())
}

func (mod *Module) TextObjectSigner(key *crypto.PublicKey) (cryptomod.TextObjectSigner, error) {
	return &TextObjectSigner{
		mod:    mod,
		scheme: crypto.SchemeBIP137,
		key:    key,
	}, nil
}

func (mod *Module) VerifyTextObjectSignature(key *crypto.PublicKey, signature *crypto.Signature, object crypto.SignableTextObject) error {
	return mod.VerifyTextSignature(key, signature, mod.formatSignableText(object))
}

func (mod *Module) formatSignableText(object crypto.SignableTextObject) string {
	commitment := base64.StdEncoding.EncodeToString(object.SignableHash()[0:15])

	return fmt.Sprintf("[%s] %s", commitment, object.SignableText())
}

// Sign signs obj as key, preferring an object (hash) signature and falling back
// to a text signature when no object-signing engine is available for the key.
func (mod *Module) Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	if signer, err := mod.ObjectSigner(key); err == nil {
		sig, err := signer.SignObject(ctx, obj)
		if err == nil {
			return sig, nil
		}
	}

	if signer, err := mod.TextObjectSigner(key); err == nil {
		return signer.SignTextObject(ctx, obj)
	}

	return nil, fmt.Errorf("no signing scheme available for key %v", key)
}

// Verify checks sig against obj using key, dispatching on the signature scheme.
func (mod *Module) Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error {
	if sig == nil {
		return errors.New("signature is nil")
	}

	switch sig.Scheme {
	case crypto.SchemeASN1:
		return mod.VerifyObjectSignature(key, sig, obj)
	case crypto.SchemeBIP137:
		return mod.VerifyTextObjectSignature(key, sig, obj)
	default:
		return fmt.Errorf("unsupported signature scheme: %s", sig.Scheme)
	}
}

// indexRepo scans and follows the given repo and attempts to index all private keys it encounters
func (mod *Module) indexRepo(ctx *astral.Context, repo objects.Repository) error {
	scan, err := repo.Scan(ctx, true)
	if err != nil {
		return err
	}

	mod.log.Logv(1, "auto-indexing %v", repo.Label())

	for objectID := range scan {
		if objectID == nil {
			continue
		}

		if objectID.Size > maxObjectSize {
			// skip large objects
			continue
		}

		object, err := mod.Objects.Load(ctx, repo, objectID)
		switch object := object.(type) {
		case nil:
			switch {
			case errors.Is(err, astral.ErrBlueprintNotFound):
				// ignore missing blueprints
			default:
				// log other errors
				mod.log.Logv(2, "indexRepo: error loading object %v from %v: %v", objectID, repo, err)
			}

		case *crypto.PrivateKey:
			// attempt to index private keys
			err = mod.indexPrivateKey(object)
			if err == nil {
				mod.log.Logv(2, "indexed private key %v (%v)", objectID, object.Type)
			}
		}
	}

	return nil
}

// indexPrivateKey attempts to index the given private key
func (mod *Module) indexPrivateKey(key *crypto.PrivateKey) error {
	keyID, err := astral.ResolveObjectID(key)
	if err != nil {
		return err
	}

	pubKey, err := mod.DerivePublicKey(mod.ctx, key)
	if err != nil {
		return err
	}

	pubKeyID, err := astral.ResolveObjectID(pubKey)
	if err != nil {
		return err
	}

	pubKeyText, err := pubKey.MarshalText()
	if err != nil {
		return err
	}

	_, err = mod.db.createPrivateKey(keyID, string(key.Type), pubKeyID, string(pubKeyText))

	return err
}

func (mod *Module) AddToIndex(object astral.Object) error {
	switch object := object.(type) {
	case *crypto.PrivateKey:
		return mod.indexPrivateKey(object)
	}

	return astral.NewErrUnexpectedObject(object)
}

func (mod *Module) AddEngine(engine cryptomod.Engine) {
	mod.engines.Add(engine)
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return cryptomod.ModuleName
}
