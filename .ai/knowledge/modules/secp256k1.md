# mod/secp256k1

Implements the `crypto.Engine` for secp256k1 keys (derives public keys, produces ASN.1 hash signers, verifies ASN.1 hash signatures) and the `secp256k1.new` op that generates a fresh private key. The `KeyType`/`MethodNew` constants, the `New()`/`PublicKey()` generators, the `FromIdentity`/`Identity` bridge between `astral.Identity` and `crypto.PublicKey`, and the typed client live in astral-go `api/secp256k1` (see astral-go .ai/knowledge/api/secp256k1.md).

## Dependencies

| Module | Why |
| --- | --- |
| `crypto` | engine auto-registered through `CryptoEngine()`; engine calls `Crypto.PrivateKey` to load the signing key from the local index (`Deps` holds only `Crypto`) |
| `astral.Node` | loader-set struct field (`mod.node`), not a `Deps` injection; not used by the engine itself, available for future ops |
| `core/assets` | `LoadYAML` reads the (currently empty) module config; `Database()` backs an unused `DB` placeholder |

## Flows

- Engine registration: loader builds the `Module` -> `mod/crypto.LoadDependencies` discovers `EngineProvider`s and calls `CryptoEngine()` -> `Engine{mod}` joins the crypto engine set.
- Public key derivation: `Engine.DerivePublicKey` delegates to the astral-go `api/secp256k1.PublicKey` helper which calls `secp256k1.PrivKeyFromBytes(...).PubKey().SerializeCompressed()`.
- Hash signer: `Engine.NewHashSigner` rejects non-`secp256k1` key types and non-`asn1` schemes -> `Crypto.PrivateKey(key)` resolves the local private key -> returns `NewHashSignerASN1(privateKey)` which holds an `ecdsa.PrivateKey` for `crypto/ecdsa.SignASN1` over `crypto/rand`.
- Hash verification: `Engine.VerifyHashSignature` checks key type/scheme -> `secp256k1.ParsePubKey` -> `ecdsa.VerifyASN1` -> returns `crypto.ErrInvalidSignature` on parse or verify failure.
- Op `secp256k1.new`: accepts a channel, sends the result of the astral-go `api/secp256k1.New()` generator, a fresh `*crypto.PrivateKey` with `Type = "secp256k1"`.

## Source

- `mod/secp256k1/module.go` - `ModuleName` and the (empty) public `Module` interface.
- `mod/secp256k1/asn1.go`, `hash_signer_asn1.go` - `SignASN1`/`VerifyASN1` package helpers and the `HashSignerASN1` type returned to `mod/crypto`.
- `mod/secp256k1/src/loader.go`, `module.go`, `deps.go`, `config.go`, `db.go` - registration, lifecycle, and (currently empty) config/DB wiring.
- `mod/secp256k1/src/engine.go` - the `crypto.Engine` implementation: `DerivePublicKey`, `NewHashSigner`, `VerifyHashSignature`.
- `mod/secp256k1/src/op_new.go` - `secp256k1.new` handler.
- The `KeyType`/`MethodNew` constants, `New()`/`PublicKey()` generators, `FromIdentity`/`Identity` bridge, and typed client live in astral-go `api/secp256k1` (see astral-go .ai/knowledge/api/secp256k1.md).

## Invariants

- Engine accepts only `key.Type == "secp256k1"` and `scheme == "asn1"`; other combinations return `crypto.ErrUnsupportedKeyType` / `ErrUnsupportedScheme` so `mod/crypto` keeps fanning out.
- `VerifyHashSignature` returns `crypto.ErrInvalidSignature` (terminal in the verify fan-out) on both `ParsePubKey` failure and ECDSA mismatch.
- The canonical `secp256k1` key-type string is defined in astral-go `api/secp256k1.KeyType` and is the value the engine, `mod/bip137sig`, and `mod/coldcard` accept.
