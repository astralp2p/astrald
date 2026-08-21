# mod/crypto

Signs and verifies hashes and text through pluggable crypto engines, and indexes private keys so callers can resolve a public key to a local signer. Owns the engine set, local node key loading, capability-based dispatch, signer lookup, object/text signing adapters, and the `crypto__private_keys` index. See `concepts/crypto-engines.md` for the fan-out contract.

## Dependencies

| Module | Why |
| --- | --- |
| `objects` | stores the node key in the system repository, loads indexed private keys via `ReadDefault`, scans configured repos for private-key objects, and holds those objects against purge |
| `dir` (opt) | injected in `Deps`; current code does not call it |
| `secp256k1` | supplies the engine via `EngineProvider`; `secp256k1.FromIdentity` (astral-go `api/secp256k1`) builds the default signer key in the sign ops |
| `core` | `core.EachLoadedModule` discovers `EngineProvider`s during `LoadDependencies` |
| `core/assets` | config, `Database()` for `crypto__private_keys`, and `Res().Read("node_key")` for the local node private key |

## Flows

* Node-key setup ordering: the loader decodes `node_key`, then the dependencies stage stores it in `Objects.System()`, then `indexPrivateKey` records its public-key mapping.
* Sign ops (`sign_hash` scheme `asn1`, `sign_text` scheme `bip137`) default the signer key to `secp256k1.FromIdentity(q.Caller())`; explicit `Key` args override.

## Invariants

* `dispatchResult`: first non-error result wins; any non-nil error means "skip me".
* `dispatchVerify`: only `ErrInvalidSignature` is terminal; every other error is "skip me"; no match returns `ErrUnsupported`.
* Capability discovery is per-call type assertion; engines implementing none of the interfaces are silently skipped.
* `formatSignableText` reads `SignableHash()[0:15]`; objects must yield at least 15 hash bytes.
* Private-key resolution is limited to keys indexed from the node key or `crypto.repos` (default `[local, system, mem0]`).
* `HoldObject` matches `crypto__private_keys.key_id` or `public_key_id` and fails closed on DB error.
* `NodeSigner` panics if no engine supplies `asn1` for the local secp256k1 identity.
* Auto-index ceiling: hard-coded `maxObjectSize = 4096`; larger object IDs are skipped during repo scan.
