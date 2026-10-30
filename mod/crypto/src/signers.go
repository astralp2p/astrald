package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// compressedKeyLen is the length of a compressed secp256k1 public key.
const compressedKeyLen = 33

// signerClaim is one claimed pair of key and scheme.
//
// why a string for the key: the claim is a map key, so it compares by value.
type signerClaim struct {
	keyType string
	key     string
	scheme  string
}

func newSignerClaim(key *crypto.PublicKey, scheme string) signerClaim {
	return signerClaim{keyType: string(key.Type), key: string(key.Key), scheme: scheme}
}

// signerRegistry answers which binding signs for a pair. It lives in memory:
// a node restart ends every binding, and apps reopen them.
//
// why a mutex and not sig.Map: admission takes all of a binding's pairs or none,
// which one map operation at a time cannot do.
type signerRegistry struct {
	mu     sync.RWMutex
	claims map[signerClaim]*signerBinding
}

// claim records every pair of b, or none when a live binding holds any of them.
func (r *signerRegistry) claim(b *signerBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.claims == nil {
		r.claims = make(map[signerClaim]*signerBinding)
	}

	for _, pair := range b.pairs {
		if _, held := r.claims[pair]; held {
			return crypto.ErrKeyClaimed
		}
	}

	for _, pair := range b.pairs {
		r.claims[pair] = b
	}

	return nil
}

// release drops the pairs of b.
func (r *signerRegistry) release(b *signerBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, pair := range b.pairs {
		if r.claims[pair] == b {
			delete(r.claims, pair)
		}
	}
}

// find returns the binding that claims the pair, or nil.
func (r *signerRegistry) find(key *crypto.PublicKey, scheme string) *signerBinding {
	if key == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.claims[newSignerClaim(key, scheme)]
}

// checkSignerKeys checks a claimed set against the rules of
// crypto.register_signer and returns its distinct pairs.
//
// Spec: .ai/system/protocols/crypto/ops/crypto.register_signer.md
func (mod *Module) checkSignerKeys(set *crypto.SignerKeys) ([]signerClaim, error) {
	if len(set.Keys) == 0 {
		return nil, errors.New("a signer claims no keys")
	}
	if len(set.Keys) > crypto.MaxSignerKeys {
		return nil, fmt.Errorf("a signer claims at most %v keys", crypto.MaxSignerKeys)
	}

	pairs := make([]signerClaim, 0, len(set.Keys))
	seen := make(map[signerClaim]struct{}, len(set.Keys))

	for _, entry := range set.Keys {
		if err := mod.checkSignerKey(entry); err != nil {
			return nil, err
		}

		pair := newSignerClaim(entry.Key, string(entry.Scheme))
		if _, dup := seen[pair]; dup {
			return nil, errors.New("a signer claims a pair twice")
		}
		seen[pair] = struct{}{}
		pairs = append(pairs, pair)
	}

	return pairs, nil
}

// checkSignerKey checks one claim.
//
// why compressed only: the registry looks a key up by its bytes, and every
// identity-derived key arrives compressed, so a second form could never match.
// why the node's key is refused: an app never stands in for the node, whatever
// the dispatch order would do with it.
func (mod *Module) checkSignerKey(entry *crypto.SignerKey) error {
	switch {
	case entry == nil || entry.Key == nil:
		return errors.New("a signer claim names no key")
	case entry.Key.Type != secp256k1.KeyType:
		return fmt.Errorf("%w: %v", cryptomod.ErrUnsupportedKeyType, entry.Key.Type)
	case entry.Scheme != crypto.SchemeBIP137 && entry.Scheme != crypto.SchemeASN1:
		return fmt.Errorf("%w: %v", cryptomod.ErrUnsupportedScheme, entry.Scheme)
	case len(entry.Key.Key) != compressedKeyLen:
		return fmt.Errorf("a secp256k1 key is %v bytes in compressed form", compressedKeyLen)
	}

	if _, err := secp.ParsePubKey(entry.Key.Key); err != nil {
		return fmt.Errorf("a signer claim names an invalid key: %w", err)
	}

	if bytes.Equal(entry.Key.Key, mod.node.Identity().PublicKey().SerializeCompressed()) {
		return cryptomod.ErrNodeKeyNotSignable
	}

	return nil
}
