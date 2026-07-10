# AI Workspace

Vendor-neutral AI context for Astrald, the daemon of the Astral Network: a
peer-to-peer layer where Identities (secp256k1 keys) expose named Services,
Queries open bidirectional Sessions over encrypted Links, and Objects are
immutable and content-addressed. Go 1.25+.

Astrald imports its primitives, wire types (`api/`), and client libraries from
`github.com/astralp2p/astral-go`; it never defines them. Protocol, wire,
and domain truth lives in the spec at `.ai/system/` (the astral-docs
submodule). These notes cover only node-side daemon internals.

## Project

```text
brontide/   Noise XK handshake
core/       Node, module manager, router
lib/        aliasgen, apphost-js, arl, paths
mod/        pluggable modules
cmd/        binaries
mobile/     gomobile-bind entry point for Android/iOS hosts
```

Config: Linux `$HOME/.config/astrald/`, macOS
`~/Library/Application Support/astrald/`. Main config `node.yaml`; per-module
`<name>.yaml`.

## Load Order

1. `.ai/README.md` — this file.
2. `.ai/rules.md` — always-on standards.

Then use the indexes; load a scoped note only when it matches the task:

* `.ai/knowledge/README.md` — repo implementation (concepts and modules).
* `.ai/patterns/README.md` — source-grounded code recipes.
* `.ai/system/README.md` — protocol and domain truth (the spec submodule).
* `.ai/artifacts/` — working notes, ignored unless explicitly referenced.

## Authority

1. User instruction
2. Code/tests
3. `.ai/system/` (the spec)
4. `.ai/rules.md`
5. `.ai/knowledge/`
6. `.ai/patterns/`
7. Referenced `.ai/artifacts/`

Call out conflicts.

## Roles

* `rules.md` — always-on standards: engineering rules, code comments, documentation style.
* `knowledge/` — repo implementation notes: `concepts/` (cross-module ideas) and `modules/` (one guide per `mod/<name>/`).
* `patterns/` — source-grounded code recipes.
* `system/` — protocol and domain truth (astral-docs submodule).
* `skills/` — project maintenance tools (`update-knowledge`, `update-protocols`).
* `artifacts/` — working notes, ignored by default.
