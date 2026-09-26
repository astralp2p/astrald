package messaging

import (
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core/assets"
	authmod "github.com/astralp2p/astrald/mod/auth"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp256k1signer "github.com/astralp2p/astrald/mod/secp256k1"
	secp256k1engine "github.com/astralp2p/astrald/mod/secp256k1/src"
	"github.com/astralp2p/astrald/resources"
	"gorm.io/gorm"
)

// Fixtures for the hosting authority: the real auth module over the test's own
// store, and a crypto module that holds the keys the test mints.

// keyring is the crypto dependency of both messaging and auth in a test. It
// holds every private key handed to AddToIndex, signs with the secp256k1 ASN.1
// signer the node uses, and verifies with the node's secp256k1 engine.
//
// why a keyring and not the crypto module: the crypto module finds a key
// through the object store and an engine another module provides. What auth
// needs of it is a signature a verifier accepts, and this makes exactly that.
//
// note: the embedded nil interface panics on any other method.
type keyring struct {
	cryptomod.Module

	mu   sync.Mutex
	keys map[string]*crypto.PrivateKey // by compressed public key, hex
}

func newKeyring() *keyring {
	return &keyring{keys: map[string]*crypto.PrivateKey{}}
}

// mint makes a key the keyring holds and answers its identity.
func (k *keyring) mint() *astral.Identity {
	key := secp256k1.New()
	_ = k.AddToIndex(key)
	return secp256k1.Identity(secp256k1.PublicKey(key))
}

func (k *keyring) AddToIndex(obj astral.Object) error {
	key, ok := obj.(*crypto.PrivateKey)
	if !ok {
		return astral.NewErrUnexpectedObject(obj)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[hex.EncodeToString(secp256k1.PublicKey(key).Key)] = key
	return nil
}

func (k *keyring) find(pub *crypto.PublicKey) (*crypto.PrivateKey, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key, ok := k.keys[hex.EncodeToString(pub.Key)]
	return key, ok
}

func (k *keyring) Sign(ctx *astral.Context, pub *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	key, ok := k.find(pub)
	if !ok {
		return nil, errors.New("the node holds no private key for this identity")
	}
	return secp256k1signer.NewHashSignerASN1(key).SignHash(ctx, obj.SignableHash())
}

func (k *keyring) Verify(pub *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error {
	if sig == nil {
		return errors.New("signature is nil")
	}
	return secp256k1engine.Engine{}.VerifyHashSignature(pub, sig, obj.SignableHash())
}

// testAssets hands a module the test's store and nothing else: no resource and
// no config file.
type testAssets struct{ db *gorm.DB }

var _ assets.Assets = testAssets{}

func (testAssets) Res() resources.Resources           { return nil }
func (testAssets) Read(string) ([]byte, error)        { return nil, os.ErrNotExist }
func (testAssets) Write(string, []byte) error         { return os.ErrPermission }
func (testAssets) LoadYAML(string, interface{}) error { return os.ErrNotExist }
func (testAssets) StoreYAML(string, interface{}) error {
	return os.ErrPermission
}
func (a testAssets) Database() *gorm.DB { return a.db }

// testAuthority is the auth module a node runs, loaded over the test's store
// the way the node loads it, with keys as its crypto. Its chain walk, contract
// index, signature checks and expiry are the ones that decide on a node.
func testAuthority(t *testing.T, db *gorm.DB, node astral.Node, keys *keyring) authmod.Module {
	t.Helper()

	loaded, err := authsrc.Loader{}.Load(node, testAssets{db: db}, testLogger())
	if err != nil {
		t.Fatalf("load auth: %v", err)
	}

	authority := loaded.(*authsrc.Module)
	authority.Crypto = keys

	return authority
}

// authorityOf answers the real auth module behind a test module's fakeAuth.
func authorityOf(mod *Module) authmod.Module {
	return mod.Auth.(*fakeAuth).Module
}

// keysOf answers the keyring a test module signs with.
func keysOf(mod *Module) *keyring {
	return mod.Crypto.(*keyring)
}

// hostedParticipant mints an identity and provisions its mailbox the way
// create_identity does: a hosting contract the identity signs, indexed with the
// real auth module, and the index row naming it.
func hostedParticipant(t *testing.T, mod *Module) *astral.Identity {
	t.Helper()

	identity := keysOf(mod).mint()

	entry, err := mod.signHosting(mod.ctx, identity)
	if err != nil {
		t.Fatalf("sign hosting: %v", err)
	}
	if err = mod.recordMailbox(identity, entry); err != nil {
		t.Fatalf("record mailbox: %v", err)
	}

	return identity
}

// hostingContracts answers the active hosting contracts the issuer gave this
// node, as the real auth module holds them.
func hostingContracts(t *testing.T, mod *Module, issuer *astral.Identity) []*auth.SignedContract {
	t.Helper()

	list, err := authorityOf(mod).SignedContracts().
		WithIssuer(issuer).
		WithSubject(mod.node.Identity()).
		WithAction(&messaging.HostMailboxAction{}).
		Find(mod.ctx)
	if err != nil {
		t.Fatalf("find hosting contracts: %v", err)
	}
	return list
}
