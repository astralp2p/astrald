# Identity

## Representation

`astral.Identity` is a compressed secp256k1 public key; its binary/hex/JSON
encoding, the `Anyone` zero value, and comparison helpers are provided by
astral-go — see astral-go .ai/knowledge/concepts/identity.md and the spec
[primitive-types/identity](../../system/primitive-types/identity.md).

## Addressing

Identity is the address. There is no hostname.

Discovery uses directory resolution and endpoint advertisement.

## User Identity

A user identity represents a human operator.

It owns a **Swarm** of Node identities that share trust, routes, and assets
(`mod/user`).
