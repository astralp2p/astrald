# Project Overview

## Summary

Astrald is a proof-of-concept P2P network (Go 1.25+) providing authenticated encrypted connections over multiple transports. Core model: Identities (secp256k1 keys) expose named Services; Queries open bidirectional Sessions; Nodes establish encrypted Links; Objects are immutable content-addressed.

## Project Structure

```text
brontide/   Noise XK protocol
core/       Node, module manager; Router embeds PriorityRouter from astral-go lib/routing
lib/        aliasgen, apphost-js, arl, paths
mod/        pluggable modules
cmd/        binaries
mobile/     gomobile-bind entry point for Android/iOS hosts
```

Primitives, wire types (`api/`), and client libraries come from
`github.com/cryptopunkscc/astral-go`.

## Config

- Linux: `$HOME/.config/astrald/`
- macOS: `~/Library/Application Support/astrald/`

Main config: `node.yaml`. Per-module config: `<name>.yaml`.
