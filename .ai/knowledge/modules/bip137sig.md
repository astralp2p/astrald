# mod/bip137sig

Implements the `crypto.Engine` for `secp256k1` keys with scheme `bip137` (BIP-39 seed generation, BIP-32 hierarchical derivation, BIP-137 Bitcoin-style message signing) plus the `bip137sig.*` op handlers that produce entropy, mnemonics, seeds, and derived keys over astral-go's `api/bip137sig` wire types. The `Entropy`/`Seed` wire objects, BIP-39 helpers, op-name constants, and typed client live in astral-go `api/bip137sig` (see astral-go .ai/knowledge/api/bip137sig.md).

## Dependencies

| Module | Why |
| --- | --- |
| `crypto` | engine auto-registered through `CryptoEngine()`; engine calls `Crypto.PrivateKey` to load the signing key matching the requested public key |
| astral-go `api/secp256k1` | reuses `secp256k1.KeyType` for keys produced by `DeriveKey` and accepted by the engine (package-level constant, not an astrald module dependency) |
| `core/assets` | loader-set struct field (`mod.assets`), not a `Deps` injection; this module reads no YAML and owns no tables |

## Flows

- Engine registration: loader builds the `Module` -> `mod/crypto.LoadDependencies` discovers `EngineProvider`s and calls `CryptoEngine()` -> `Engine{mod}` joins the crypto engine set.
- Text signer: `Engine.NewTextSigner` rejects non-`bip137` scheme and non-`secp256k1` key types -> `Crypto.PrivateKey(key)` resolves the local private key -> `isCompressedPublicKey` chooses the 33- or 65-byte form -> returns `MessageSigner{key, compressed}` wrapping a `btcec.PrivateKey`.
- Sign text: `MessageSigner.SignText` builds the Bitcoin signed-message preimage with `formatBitcoinMessage` (length-prefixed `Bitcoin Signed Message:\n` prefix + length-prefixed message) -> double SHA-256 -> `ecdsa.SignCompact` -> returns a `crypto.Signature{Scheme: "bip137", Data: 65-byte compact sig}`.
- Verify text: `Engine.VerifyTextSignature` checks key type/scheme -> `secp.ParsePubKey` -> base64-encodes the compact signature and calls `bip-0137/verify.VerifyWithPubKey` -> returns `crypto.ErrInvalidSignature` on parse failure, verify error, or `false` result.
- Generate seed: `Module.GenerateSeed` calls the astral-go `api/bip137sig` helpers `NewEntropy(DefaultEntropyBits=128)` -> `EntropyToMnemonic` -> `MnemonicToSeed(words, "")` (PBKDF2-HMAC-SHA512, 2048 rounds, 64-byte output).
- Derive key: `Module.DeriveKey(seed, path)` parses the BIP-32 path via the astral-go `api/bip137sig` helper `ParseDerivationPath` (supports `m/`, hardened `'`/`h`) -> `hdkeychain.NewMaster(seed, MainNetParams)` -> iterates `Derive(idx)` -> returns `crypto.PrivateKey{Type: "secp256k1", Key: serialized}`.
- Ops: `bip137sig.new_entropy` returns a random `Entropy` of the requested bit length; `bip137sig.mnemonic` converts an `Entropy` to a 12-24 word `String16`; `bip137sig.seed` converts a mnemonic `String16` plus optional passphrase into a `Seed`; `bip137sig.derive_key` derives a `crypto.PrivateKey` from a streamed `Seed` and `Path` arg.

## Source

- `mod/bip137sig/module.go` - `ModuleName` and the public `Module` interface (`GenerateSeed`, `DeriveKey`), typed over astral-go `bip137sig.Seed` / `crypto.PrivateKey`.
- `mod/bip137sig/src/loader.go`, `module.go`, `deps.go` - registration, lifecycle, `GenerateSeed`/`DeriveKey`, and dependency wiring.
- `mod/bip137sig/src/engine.go` - the `crypto.Engine` text-signer provider and verifier.
- `mod/bip137sig/src/message_signer.go` - Bitcoin signed-message hashing and `ecdsa.SignCompact` producer.
- `mod/bip137sig/src/op_new_entropy.go`, `op_mnemonic.go`, `op_seed.go`, `op_derive_key.go` - query operation handlers.
- The `Entropy` / `Seed` wire objects, BIP-39/BIP-32 helpers (`NewEntropy`, `EntropyToMnemonic`, `MnemonicToSeed`, `ParseDerivationPath`, `DefaultEntropyBits`), op-name constants, seed/entropy sentinels, and typed client live in astral-go `api/bip137sig` (see astral-go .ai/knowledge/api/bip137sig.md).

## Invariants

- Engine accepts only `scheme == crypto.SchemeBIP137` and `key.Type == secp256k1.KeyType`; other combinations return `ErrUnsupportedScheme` / `ErrUnsupportedKeyType` so `mod/crypto` keeps fanning out.
- `VerifyTextSignature` returns `crypto.ErrInvalidSignature` (terminal) on parse failure, verify error, or `ok == false`.
- `MessageSigner.compressed` is derived from public-key length: 33 bytes -> compressed, 65 bytes -> uncompressed, anything else -> error before signing.
- `Entropy`/`Seed` length enforcement lives with the wire objects in astral-go `api/bip137sig` (see astral-go .ai/knowledge/api/bip137sig.md).
- `DeriveKey` uses Bitcoin MainNet parameters; an empty or `m` path returns the master key without descent.
- `GenerateSeed` always uses an empty BIP-39 passphrase; `MnemonicToSeed` exposes the passphrase argument for callers that need one.
