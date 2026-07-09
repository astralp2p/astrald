# Operation Patterns

Node-side operation handlers exposed through `astral-query`. The op framework
(`routing.IncomingQuery`, `AddStructPrefix`, `channel`) is imported from `lib/routing`
and `astral/channel`; this note covers only the astrald `mod/*/src` side.

## Handler

* Method names use `Op` + PascalCase, registered via `AddStructPrefix(s, "Op")`.
* Operation names use snake_case, converted by `log.ToSnakeCase`.
* Handler signature: `func(*astral.Context, *routing.IncomingQuery[, args]) error`.
* `q.AcceptRaw()` returns `io.ReadWriteCloser`; pass it to `channel.New(...)`.
* `q.Accept(cfg...)` is the same flow in one call.

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

## Args Struct

Args are parsed from the query string into the third handler argument.

* `query:"required"` fields are enforced before the handler runs.
* Other fields are taken when present and left zero when absent.

```go
type opPunchArgs struct {
    Target string                  // present when set in query string
    In     string `query:"optional"`
    Out    string `query:"optional"`
}
```

```go
type findArgs struct {
    ID  *astral.ObjectID `query:"required"`
    Out string           `query:"optional"`
}
```

## Client

Protocol clients live in astral-go `api/<p>/client/`.

## Call Boundary

Choose the call path by caller situation.

| Situation | Use |
|---|---|
| Module running on same node | Dependency interface, e.g. `mod.Dir.ResolveIdentity(name)` |
| Operation on a different node | Client with target, e.g. `natclient.New(target, astrald.Default())` |
| External app with no node access | Default client routed through apphost |
