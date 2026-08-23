# AI Workspace

Astrald is the reference daemon of the Astral Network: a peer-to-peer layer
where Identities (secp256k1 keys) expose named Services, Queries open
bidirectional Sessions over encrypted Links, and Objects are immutable and
content-addressed. Go 1.25+.

## Load Order

1. `.ai/rules.md` — always-on standards.
2. `.ai/system/` — the spec (astral-docs submodule): protocol, wire, and domain truth.
3. Code and tests — implementation truth.

There are no curated knowledge or pattern notes. Read module source under
`mod/<name>/` for daemon internals; non-obvious decisions live in the source
as `// why:` comments, known gaps as `// fixme:` / `// todo:`.

## Project Layout

```text
brontide/   Noise XK handshake
core/       Node, module manager, router
lib/        aliasgen, apphost-js, arl, paths
mod/        pluggable modules
cmd/        binaries
mobile/     gomobile-bind entry point for Android/iOS hosts
```

Primitives, wire types (`api/`), and client libraries live in
`github.com/astralp2p/astral-go` and are cited by package, never restated.

Config: Linux `$HOME/.config/astrald/`, macOS
`~/Library/Application Support/astrald/`. Main config `node.yaml`; per-module
`<name>.yaml`.

## Authority

1. User instruction
2. Code/tests
3. `.ai/system/` (the spec)
4. `.ai/rules.md`

Call out conflicts.
