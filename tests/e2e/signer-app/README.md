# signer-app

An app holds a key the node does not, claims it on a `crypto.register_signer`
binding, and signs for it. This is the path a hardware signer takes.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode, because the flow needs local signing and astral-py has no curve
  math without `coincurve`. The driver claims `signer1` with a User key that only
  the driver holds, so the contract carries mixed schemes: `bip137` from the app
  and `asn1` from the node. It then probes the rules of a binding.
- **oracle** `verify.py` — every step held, and `signer1` still answers as the
  User's node after the app is gone, while nothing signs under the User's key.

A key the node holds never reaches an app, so the driver also claims an app's own
key, which the node holds, and confirms the app sees no request for it.

`signer1` is this test's own node. The test leaves it claimed by a User whose key
no longer exists anywhere.
