# Crypto Engine Refactor Plan

## Problem

`mod/crypto.Engine` currently combines several unrelated cryptographic capabilities:

- deriving a public key from a private key
- creating hash signers
- verifying hash signatures
- creating text signers
- verifying text signatures

Implementations can support only a subset, so they embed `NilEngine` and return `ErrUnsupported` for the rest. The crypto module then probes every registered engine until one succeeds.

This makes dispatch implicit and error-prone:

- real failures can be hidden as `ErrUnsupported`
- engine selection depends on registration order
- signing backends and verification algorithms are conflated
- multiple providers for the same operation have no explicit selection policy
- signer factories cannot use the caller context, so implementations create detached contexts internally
- `crypto.public_key` currently bypasses the engine path and hardcodes secp256k1

## Proposed Design

Replace the broad `Engine` interface with small capability interfaces registered through a typed registry.

```go
type SignatureSpec struct {
	KeyType string
	Scheme  string
}

type KeyDeriver interface {
	KeyType() string
	PublicKey(ctx *astral.Context, key *PrivateKey) (*PublicKey, error)
}

type HashSignerProvider interface {
	Spec() SignatureSpec
	Priority() int
	HashSigner(ctx *astral.Context, key *PublicKey) (HashSigner, error)
}

type HashVerifier interface {
	Spec() SignatureSpec
	VerifyHashSignature(key *PublicKey, sig *Signature, hash []byte) error
}

type TextSignerProvider interface {
	Spec() SignatureSpec
	Priority() int
	TextSigner(ctx *astral.Context, key *PublicKey) (TextSigner, error)
}

type TextVerifier interface {
	Spec() SignatureSpec
	VerifyTextSignature(key *PublicKey, sig *Signature, msg string) error
}

type CryptoRegistry interface {
	AddKeyDeriver(KeyDeriver) error
	AddHashSignerProvider(HashSignerProvider) error
	AddHashVerifier(HashVerifier) error
	AddTextSignerProvider(TextSignerProvider) error
	AddTextVerifier(TextVerifier) error
}

type CryptoProvider interface {
	RegisterCrypto(CryptoRegistry) error
}
```

The crypto module should store explicit registries:

```go
keyDerivers   map[string]KeyDeriver
hashSigners   map[SignatureSpec][]HashSignerProvider
hashVerifiers map[SignatureSpec]HashVerifier
textSigners   map[SignatureSpec][]TextSignerProvider
textVerifiers map[SignatureSpec]TextVerifier
```

Dispatch becomes deterministic:

- public-key derivation looks up `privateKey.Type`
- hash verification looks up `(publicKey.Type, signature.Scheme)`
- text verification looks up `(publicKey.Type, signature.Scheme)`
- hash signing looks up `(publicKey.Type, scheme)` and tries matching signer providers by explicit priority
- text signing does the same for text signer providers

## Error Model

Use separate errors for unsupported operations and temporarily unavailable signing backends.

Suggested additions:

```go
var (
	ErrSignerUnavailable = errors.New("signer unavailable")
	ErrDuplicateProvider = errors.New("duplicate crypto provider")
)
```

Verification should not probe unrelated providers. Once a matching verifier is found, return its result directly. If no verifier matches the key type and signature scheme, return `ErrUnsupported`.

Signing may try multiple matching providers, but only fallback on `ErrSignerUnavailable`. Other errors should be returned immediately because they usually mean malformed input, storage failure, hardware failure, or cancellation.

## Migration Plan

1. Add capability interfaces and registry types in `mod/crypto`.

2. Replace `engines sig.Set[crypto.Engine]` in `mod/crypto/src.Module` with typed registries keyed by key type and `SignatureSpec`.

3. Implement registry methods on `mod/crypto/src.Module`.

4. Add support for `CryptoProvider.RegisterCrypto(registry)` in dependency loading.

5. Keep `EngineProvider` temporarily through an adapter so existing providers can continue to work during migration.

6. Update crypto module dispatch:
   - `PublicKey(ctx, key)` uses the key-deriver registry.
   - `HashSigner(ctx, key, scheme)` uses matching hash signer providers in priority order.
   - `TextSigner(ctx, key, scheme)` uses matching text signer providers in priority order.
   - `VerifyHashSignature(key, sig, hash)` uses one matching hash verifier.
   - `VerifyTextSignature(key, sig, msg)` uses one matching text verifier.

7. Change public module methods and call sites so signer factories receive context:
   - `HashSigner(ctx, key, scheme)`
   - `TextSigner(ctx, key, scheme)`

8. Migrate `mod/secp256k1`:
   - register a key deriver for `secp256k1`
   - register an ASN.1 hash signer provider for `secp256k1/asn1`
   - register an ASN.1 hash verifier for `secp256k1/asn1`

9. Migrate `mod/bip137sig`:
   - register a BIP-137 text signer provider for `secp256k1/bip137`
   - register a BIP-137 text verifier for `secp256k1/bip137`

11. Fix `OpPublicKey` so it calls `mod.PublicKey(ctx, key)` instead of `secp256k1.PublicKey(key)`.

12. Update object signers, text object signers, RPC operations, and `NodeSigner` for context-aware signer creation.

13. Remove `NilEngine` and `EngineProvider` after all providers are migrated.

14. Add tests for:
    - registry lookup by private key type
    - hash verifier lookup by key type and scheme
    - text verifier lookup by key type and scheme
    - propagation of malformed-key and malformed-signature errors
    - fallback only on `ErrSignerUnavailable`
    - deterministic signer provider priority
    - `OpPublicKey` using the registry rather than hardcoded secp256k1

## Settled Decision

A local private key signs before an app holding a `crypto.register_signer` binding. The binding is not an engine, so it takes no part in the provider priority above.
