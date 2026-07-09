# Operation Patterns

Use this pattern for node-side operation handlers exposed through
`astral-query`. The op framework itself (`routing.IncomingQuery`,
`AddStructPrefix`, `channel`) is imported from
`github.com/cryptopunkscc/astral-go` (`lib/routing`, `astral/channel`); this
note covers only the astrald `mod/*/src` side.

## Handler

Invariants:

- Method names use `Op` + PascalCase (registered via `AddStructPrefix(s, "Op")`).
- Operation names use snake_case (auto-converted by `log.ToSnakeCase`).
- Handler signature: `func(*astral.Context, *routing.IncomingQuery[, args]) error`.

```go
func (mod *Module) OpSessions(ctx *astral.Context, q *routing.IncomingQuery, args opSessionsArgs) (err error) {
    ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
    defer ch.Close()

    for _, s := range mod.sessions() {
        if err = ch.Send(s); err != nil {
            return ch.Send(astral.NewError(err.Error()))
        }
    }
    return ch.Send(&astral.EOS{})
}
```

`q.AcceptRaw()` returns `io.ReadWriteCloser`; pass it to `channel.New(...)`.
`q.Accept(cfg...)` is the same flow in one call.

Source: `mod/nodes/src/op_sessions.go`, `mod/nodes/src/op_links.go`

## Args Struct

Args are parsed from the query string into the third handler argument. Fields
with `query:"required"` are enforced before the handler runs; otherwise the
field is taken when present and left zero when absent.

```go
type opPunchArgs struct {
    Target string                  // present when set in query string
    In     string `query:"optional"`
    Out    string `query:"optional"`
}
```

For strictly required fields use `required`:

```go
type findArgs struct {
    ID  *astral.ObjectID `query:"required"`
    Out string           `query:"optional"`
}
```

The tag-semantics detail (`Op.invoke` enforcement of `query:"required"`,
`optional` as a documentation marker) lives in astral-go — see astral-go
`.ai/patterns/operations.md`.

Source: `mod/nat/src/op_punch.go`

## Client

Typed clients no longer live in astrald. Each protocol's client package (the
`Client`/`Default()` constructors and one operation per file) lives in
astral-go `api/<name>/client/` — see astral-go `.ai/patterns/operations.md` for
the client and client-operation-file recipes.

## Call Boundary

Choose the call path by caller situation.

| Situation | Use |
|---|---|
| Module running on same node | Dependency interface, e.g. `mod.Dir.ResolveIdentity(name)` |
| Operation on a different node | Client with target, e.g. `natclient.New(target, astrald.Default())` |
| External app with no node access | Default client routed through apphost |

The client packages (`natclient` above and the rest) live in astral-go
`api/<name>/client`, not in astrald.

Source: `mod/nat/src/op_punch.go`, `mod/nat/src/op_node_consume_hole.go`
