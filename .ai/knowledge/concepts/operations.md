# Operations

An Op is a named module service. A Query invokes it by method string. Ops can
be called locally, remotely, or by an App.

## Naming

* Define `Op<Name>` on `Module`.
* Expose it as `module.name`; PascalCase is converted to snake_case.
* Put the implementation in `op_<name>.go`.

## Structure

Op discovery, signature validation, arg-struct parsing (via `query` struct
tags), and required-arg rejection are provided by astral-go
`github.com/cryptopunkscc/astral-go/lib/routing` (`NewOp`,
`OpRouter.AddStructPrefix`) and `lib/query` — see astral-go
.ai/knowledge/concepts/operations.md.

## Flow

* Accept the query.
* Wrap it in a Channel.
* Read or compute data.
* Stream typed Objects.
* End with `EOS` or `Ack`.
