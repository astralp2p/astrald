# Crypto Engines

`mod/crypto` is the node-wide signing boundary. No module calls a crypto library directly; all signing goes through the `mod/crypto` engine fan-out so private keys can live outside software (e.g. a hardware wallet the OS cannot read). `mod/crypto` delegates to whichever engine claims the key.

## EngineProvider

* A module that provides a signing backend implements `EngineProvider`: `CryptoEngine() Engine`.
* `Engine` is an opaque `any` that implements any subset of the capability interfaces in `mod/crypto/engine.go`:

| Interface | Capability |
|---|---|
| `PublicKeyDeriver` | derive a public key from a private key |
| `HashSignerProvider` | return a `HashSigner` bound to a key and scheme |
| `HashVerifier` | verify a hash signature |
| `TextSignerProvider` | return a `TextSigner` bound to a key and scheme |
| `TextVerifier` | verify a text signature |

* `mod/crypto.LoadDependencies` walks loaded modules, calls `CryptoEngine()` on each `EngineProvider`, and adds the result to its engine set.
* Every public method dispatches per capability. Capability is detected by type assertion, not by error: an engine that does not implement the requested interface is silently skipped.

## Fan-out result protocol

* Sign / derive: the first engine returning a non-error result wins. Any returned error means "skip me, keep looking". No match returns `ErrUnsupported`.
* Verify: any engine returning `nil` succeeds. `ErrInvalidSignature` is terminal ("the key is mine and the signature is wrong"). Any other error means "skip me, keep looking". No match returns `ErrUnsupported`.

## Signing paths

* `HashSigner`, scheme `asn1`: signs an opaque byte hash. Machine-verified. Use for node authentication, object signatures, and any path where no human reviews the result.
* `TextSigner`, scheme `bip137`: signs a human-readable string. Human-consent path. Before signing, `mod/crypto` formats the content as `[<hash-prefix>] <text>`; a hardware wallet displays this string before the user approves. Use when a human must read and approve the commitment.
