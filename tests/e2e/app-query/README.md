# app-query

An app served on node1 answers a query node2 issues, and the object it stores
as a side effect is on node1 afterwards.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — serve a handler on node1 through `Client.serve`,
  query it from node2, close the service.
- **oracle** `verify.py` — the answer is the id the payload hashes to, and
  node1's `local` repo holds exactly those bytes.

An app extends the node that hosts it: it registers a handler through
`apphost.bind` and `apphost.register_handler`, and astrald routes queries to
it. That is the platform's core promise and it had no coverage — every existing
test drives ops the node itself implements, so a regression in app routing was
invisible to the whole tree while `main.suite` stayed green.

## The identity the handler answers for

`apphost.register_handler` takes no identity: astrald registers the handler
under the query's caller, so the identity the serving connection authenticates
as **is** the identity the app answers for. node2 targets node1, so the handler
is served over node1's own token.

Registration and routing key on different things: a handler is registered
under the *registering* connection's caller
(`mod/apphost/src/op_register_handler.go:27`), while an inbound query is
matched on its **target** (`query_router.go:17`).

The first draft served over `user_token` — node1's User identity — and the
sibling's query to node1 hung until the client's deadline. Nothing was wrong
with the routing; the handler was simply registered under an identity the query
never resolved to.

Reaching an app under an identity **of its own** is deferred, and it is a
contract question rather than a routing one: a peer can only route to an app
identity that a `mod.nodes.relay_for_action` contract names, and the
`two-nodes` state has none, so `query_preprocessor.go` runs here with both
branches inert. This test is about apps; it is not about app identity.

`user_token` also authenticates on node1 alone. node2 asks as itself, which is
the caller this test wants anyway: a swarm member reaching an app on a peer.

## One answer, two independent claims

The app answers the ObjectID of what it was sent and stores those bytes as it
goes, so a single round trip carries two things the oracle can check without
asking the app anything:

- the id is a content hash the oracle recomputes from the payload itself;
- the bytes are in node1's repository, or they are not.

Either alone is weak. The answer could come from an app that hashed and stored
nothing; the object could have been put there by anything. Together they say a
query reached the app, was served under an identity that could write, and came
back.

The service is closed before the driver exits, so the app is gone by the time
the oracle runs. The oracle judges what was left behind, never a live process
it could interrogate.

## The app is offered every query node1 receives

Worth knowing before reading the handler: apphost registers at
`RoutingPriorityHigh`, so while an app is registered under node1's identity the
node offers it **every** query targeting node1 — its own ops included — before
the op router sees them. The app declines what it has not mounted, that
declination is `route_not_found` rather than a rejection, and the priority
router falls through to the real op.

One consequence is invisible in the code: the handler's own `objects.create`
is dispatched back into the app before reaching the objects module. This test
exercises apphost re-entrancy while answering, and survives it because
astral-py serves each inbound connection on its own task, leaving the accept
loop free while a handler awaits.

## Traps

`serve()` returns only once the first registration cycle has completed, so
there is no registration race to sleep through. A wait added to compensate for
a race the SDK already closes would make this test green for the wrong reason.
The op is mounted just after, and a query arriving in that window would be
skipped rather than queued — harmless here, since the sibling connects
afterwards.

The query carries an explicit 15 s deadline. A broken route does not answer, it
hangs, and the client's default is 60 s; the verdict is the same either way and
this one arrives four times sooner.

The payload carries no spaces or colons. The wire form is `op?param=value`, and
a value that needs escaping is a second thing to get wrong in a test that is
not about query-string encoding.
