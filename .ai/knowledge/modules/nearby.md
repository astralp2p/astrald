# mod/nearby

Discovers peers on the local network by exchanging status and scan messages over the ether broadcast path. Owns the discovery mode, status composition, the lazy-expiring cache of observed peers by source IP, and resolver hooks that make nearby aliases and endpoints visible to `dir` and `nodes`.

## Dependencies

| Module | Why |
|---|---|
| `ether` | broadcasts status/scan and delivers received broadcasts through objects |
| `objects` | registers `ReceiveObject`; rejects broadcast objects that must not be stored |
| `user` | `Run` waits for `Ready`; `Mode` falls back to visible before identity exists; stealth resolution uses the local user identity |
| `dir` | registers the nearby alias resolver and contributes the local alias to composed status |
| `nodes` | registers the endpoint resolver over cached status attachments |
| `tree` | persists and follows the discovery mode at `/mod/nearby/mode` |
| `auth` | resolves an identity from signed contract attachments |
| `core` | dependency injection and loaded-module scan for `nearby.Composer` implementations |
| `exonet`, `shell`, `tcp` | injected dependencies kept available for composers and local integrations |

## Invariants

* `ReceiveObject` acts only on ether self-echo events; drops not sent by the local node are ignored — this is the mechanism that drives nearby discovery.
* `Cache()` performs lazy eviction for entries older than `statusExpiration`.
* The periodic updater must broadcast about five seconds before expiry, or normal entries time out.
* `Mode()` returns visible while the user identity is still nil.
* Stealth mode with zero attachments is suppressed and behaves like silent mode for broadcast.
* Attachments larger than `MaxAttachmentSize` are rejected with `ErrObjectTooLarge`.
* YAML `mode` applies only when configured; otherwise the persisted tree value is authoritative.
* `nearby.Composer` is the extension point; its two resolver hooks are dir dot-prefixed alias lookup and nodes endpoint lookup.
