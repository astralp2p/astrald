# mod/crypto

Signs and verifies hashes and text through pluggable crypto engines, and indexes private keys so callers can resolve a public key to a local signer. Owns the engine set, local node key loading, capability-based dispatch, signer lookup, object/text signing adapters, and the `crypto__private_keys` index.

## Dependencies

| Module | Why |
| --- | --- |
| `objects` | stores the node key in the system repository, loads indexed private keys through `ReadDefault`, scans configured repositories for private-key objects, and exposes private-key object holds for purge |
| `dir` (opt) | injected in `Deps`; current crypto code does not call it |
| `secp256k1` | the engine arrives via `EngineProvider` (`mod/secp256k1`); `secp256k1.FromIdentity(q.Caller())` — the astral-go `api/secp256k1` package helper, not a module call — builds the default signer key in `sign_hash`/`sign_text` ops |
| `core` | `core.EachLoadedModule` discovers `EngineProvider`s during `LoadDependencies` and registers their engines |
| `core/assets` | `LoadYAML` reads crypto config, `Database()` backs `crypto__private_keys`, and `Res().Read("node_key")` loads the local node private key |

## Flows

- Engine discovery: `LoadDependencies` injects `Objects` and optional `Dir` -> `core.EachLoadedModule` scans loaded modules for `crypto.EngineProvider` -> `AddEngine` adds each returned engine to `engines sig.Set[Engine]`.
- Node key setup: loader reads `node_key` from resources -> decodes a `PrivateKey`; dependencies stage stores it in `Objects.System()` -> `indexPrivateKey` records its public key mapping.
- Repository key indexing: `Run` starts one goroutine per configured repository -> `repo.Scan(ctx, true)` -> skip object IDs larger than 4096 bytes -> load private keys -> derive public key through engines -> insert `crypto__private_keys` row.
- Private key lookup: `PrivateKeyID` marshals the public key text -> `findPrivateKeyByPublicKey` returns the matching row; `PrivateKey` then loads `KeyID` through `Objects.ReadDefault()` and requires a `*crypto.PrivateKey` object.
- Capability dispatch: `dispatchResult[Cap, R]` clones `engines`, type-asserts each entry to `Cap` (e.g. `HashSignerProvider`), and returns the first non-error result; on no match returns `ErrUnsupported`. `dispatchVerify[Cap]` is the verify-only variant: any engine returning nil wins, `ErrInvalidSignature` short-circuits.
- Object holding: `HoldObject` returns true for any object ID matching either `key_id` or `public_key_id` in `crypto__private_keys` so `objects.purge` cannot remove an indexed private key or its derived public key.
- Public key derivation: `DerivePublicKey` dispatches `PublicKeyDeriver` across engines and returns the first successful key.
- Hash signing: `crypto.sign_hash` defaults scheme to `asn1` and signer key to `secp256k1.FromIdentity(q.Caller())` -> optional `Key`/`Hash` args override channel input -> `NewHashSigner` dispatches `HashSignerProvider` across engines -> `SignHash` sends the signature.
- Text signing: `crypto.sign_text` follows the same shape with default scheme `bip137`, dispatching `TextSignerProvider` and accepting `String8`/`String16` text frames or a `Text` arg.
- Signature verification: `VerifyHashSignature`/`VerifyTextSignature` reject nil or empty public keys, signatures, and payloads, then run `dispatchVerify` over `HashVerifier`/`TextVerifier`; `ErrInvalidSignature` is terminal, other errors are skipped, no match returns `ErrUnsupported`.
- Object signing: `ObjectSigner.SignObject` wraps `NewHashSigner(key, asn1).SignHash(object.SignableHash())`; `TextObjectSigner.SignTextObject` wraps `NewTextSigner(key, bip137).SignText(formatSignableText(object))` where the text is `"[<base64 hash[0:15]>] <SignableText>"`.
- Node signing: `NodeSigner` builds the local `secp256k1` public key from `node.Identity().PublicKey().SerializeCompressed()` and asks for an `asn1` hash signer; panics if no engine provides one.

## Source

- `mod/crypto/module.go`, `engine.go`, `errors.go` - public module interface, engine capability contracts (`PublicKeyDeriver`, `HashSignerProvider`, `HashVerifier`, `TextSignerProvider`, `TextVerifier`), and sentinels.
- Crypto object types (`PrivateKey`, `PublicKey`, `Signature`, `Hash`, `SignableObject`), `Method*` op-name constants, `Scheme` constants (`asn1`/`bip137`), and the typed client live in astral-go `api/crypto/` (see astral-go .ai/knowledge/api/crypto.md).
- `mod/crypto/src/loader.go`, `module.go`, `deps.go`, `config.go` - registration, node key loading, dependency injection, engine fan-out wiring, and indexing lifecycle.
- `mod/crypto/src/dispatch.go` - generic `dispatchResult` and `dispatchVerify` helpers used by every public method.
- `mod/crypto/src/db.go`, `db_private_key.go`, `object_holder.go` - private-key index schema, lookup helpers, and cleanup hold hook.
- `mod/crypto/src/object_signer.go`, `text_object_signer.go` - object and text-object signing adapters.
- `mod/crypto/src/op_public_key.go`, `op_sign_hash.go`, `op_sign_text.go`, `op_verify_hash_signature.go`, `op_verify_text_signature.go` - query operation handlers.

## Surface

| What | Why it matters |
| --- | --- |
| `crypto.Engine` (opaque `any`) and `EngineProvider` | extension point: an engine implements any subset of `PublicKeyDeriver`, `HashSignerProvider`, `HashVerifier`, `TextSignerProvider`, `TextVerifier` |
| `HashSigner`, `TextSigner`, `ObjectSigner`, `TextObjectSigner` | signer abstractions used by auth, ether, and other modules |
| `crypto.public_key`, `crypto.sign_hash`, `crypto.sign_text` | query methods for deriving public keys and producing signatures |
| `crypto.verify_hash_signature`, `crypto.verify_text_signature` | query methods and module paths for validating signatures |
| `objects.Holder` | prevents indexed private-key objects from being purged |
| `crypto__private_keys` | maps public-key text to private-key object IDs for local signing |

## Invariants

- Engines self-filter by key type/scheme; in `dispatchResult` any non-nil error means "skip me"; in `dispatchVerify` only `ErrInvalidSignature` is terminal, every other error is treated as "skip me".
- Capability discovery is per-call type assertion: engines implementing none of the capability interfaces are silently skipped.
- `formatSignableText` reads `SignableHash()[0:15]`; objects must yield at least 15 hash bytes.
- Private-key resolution limited to keys indexed from node key or `crypto.repos` (default `[local, system, mem0]`).
- `HoldObject` matches `crypto__private_keys.key_id` or `public_key_id` and fails closed on DB errors.
- `NodeSigner` panics if no engine supplies `asn1` for the local secp256k1 identity.
- Auto-index ceiling: hard-coded `maxObjectSize = 4096`.
- Encodings: text/binary forms of `PrivateKey`, `PublicKey`, `Signature`, and `Hash` are specified in [system/protocols/crypto/types](../../system/protocols/crypto/types).
