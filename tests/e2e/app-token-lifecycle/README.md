# app-token-lifecycle

An apphost access token works until it expires or is deleted, and then it
opens no new session.

- **env** node · **start** `bootstrap-user-software-key` · **saves** —
- **driver** `script.py` — as the User on node1 it mints two tokens for the
  User with `apphost.create_token`: `short` with `duration=3s` and `doomed`
  with ten minutes. It opens a session with each straight away (handshake and
  `apphost.whoami`), then opens one more session with each and holds it. It
  deletes `doomed` with `apphost.delete_token` and opens a session with it
  again. It waits until five seconds have passed since minting `short` and
  opens a session with that. Last it sends `apphost.whoami` on each held
  session and lists the User's tokens.
- **oracle** `verify.py` — the minted lifetime is about 3 s; both tokens opened
  a session as the User inside that lifetime; the deleted token and the
  expired token each fail the handshake with `AuthFailed` (not a timeout or a
  lost node); node1, asked again by the oracle, still refuses both, still
  accepts the User's own token, and no longer lists the deleted token.

## What is recorded and not judged

A session authenticated before its token ended keeps answering after it:
`Guest.onAuthTokenMsg` checks the token once, at the handshake, and nothing
re-checks it per query (`mod/apphost/src/guest.go`). The driver queries
`apphost.whoami` on a held session of each token after the expiry and the
deletion, and the oracle prints what came back. It does not assert it: the
behaviour is a property of the code today, and a later change that ends
sessions with their tokens would be a fix, not a regression.

Closing a session does not remove the handlers it registered
(`mod/apphost/src/query_router.go`), so nothing about handlers is asserted
either.

## Why 3 s and a 5 s wait

`FindAccessToken` selects rows with `expires_at > now`
(`mod/apphost/src/db.go`), so a token is refused the moment its time passes.
The driver waits until five seconds after the mint, two beyond the lifetime, so
a slow host or a clock step of a second cannot make a live token look expired.
The same margin runs the other way: the first probes must finish inside the
3 s lifetime, and the driver stops with a message when the host was too slow,
since "worked before" is then no evidence.

## Why the refusals are checked as `AuthFailed`

A dial that fails, a timeout and an unreachable node all leave a probe with no
session. The handshake answers a bad token with `error_msg{auth_failed}`, which
the client raises as `AuthFailed`; only that counts as the refusal. The oracle
also checks the same two tokens itself, after the driver has finished, so the
driver's record is not the only evidence.

## Concurrency

The test names no app, service or handler, mints two tokens for the User, and
deletes one of them. It leaves the other, expired, in node1's token table
(expired rows stay listable until deleted). A later test that counts the User's
tokens would see it; none does today (`apphost-origin` asks only that the
count is above zero).
