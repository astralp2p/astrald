# claim-race — of many claims at one instant, exactly one wins

A node is claimed once. The user module's `claim()` checks for an active
contract and sets one under `mod.mu`, so the check and the set are one
transition (`mod/user/src/contracts.go`). Without the lock or the check, two
claims that arrive together both pass, the second contract replaces the first,
and the node answers two Users that each believe they own it. This test fires
twelve distinct Users' claims at one fresh node at once, on four nodes, and
holds each node to one owner.

- **env** node · **start** `null` · **saves** — · **destroys** its world: it
  claims every node of its roster
- **roster** `race1` … `race4`, one race per node, all under the harness's E2E
  gate
- **driver** `script.py` — per node, with the node's token: makes twelve Users
  from new BIP-39 words. A contender claims with `user.bootstrap` (words in the
  body) or with `user.accept_contract` (a claim contract the node signed in
  advance through `auth.sign_contract`, with the User's key stored). `race1`
  and `race2` race twelve bootstraps, `race3` twelve accept_contracts, `race4`
  six of each. Every claim query is opened first, so each op has passed its own
  `node already claimed` check and waits for its body; half a second later the
  bodies go out together, and every claim reaches `claim()` on an unclaimed
  node. Each answer is recorded: won, refused with the node's text, a hang past
  30 s, or a transport error. Then a late User claims through each op; the
  driver reads the answer before it sends the body.
- **oracle** `verify.py` — per node, first from the node: the active contract
  in the tree store (`/mod/user/config/active_contract`) is a contender's, with
  the node as subject; the node's log announces exactly one activation
  (`hello, <user>!`), for that issuer; `user.info` reads `claimed` and the same
  contract by ObjectID; `apphost.list_tokens` holds a token for no claiming User
  but the issuer, and for the issuer only when it claimed through
  `user.bootstrap`; the oracle's own `user.bootstrap` is refused as
  `node already claimed` and leaves the active contract as it was. Then from the
  facts: exactly one contender won and it is that issuer; the eleven others
  were refused with `the node has an active contract`, `node already claimed`
  or `claim refused: the claim rule of auth.sign_contract refuses this user`,
  none hung and none broke; both late claims were refused before the op read
  their body.

## Why the rounds differ

In a mixed round one claim wins, most often an accept_contract, signed in
advance (1 bootstrap win in 15 mixed rounds measured), and the bootstraps still
deriving their key then fail the claim rule in `signClaim` before `claim()`.
`race1` and `race2` therefore race bootstraps alone, so their eleven losers
reach `claim()` and lose there; `race3` races accept_contracts alone; `race4`
races the two ops against each other.

## What it catches

Measured at `9f647ff1` on a Raspberry Pi host under load from other runs:

| mutation of `claim()` | runs failed | rounds with more than one winner |
|-----------------------|-------------|----------------------------------|
| `mod.mu` not taken | 6 of 6 | 22 of 24 |
| the active-contract check removed | 3 of 3 | 12 of 12 |

Each failure is the oracle's node-side assertion, `the node activated N
contracts, want one`.

## Not checked

A losing bootstrap stores and indexes its User's key before `claim()` refuses
it (`mod/user/src/op_bootstrap.go`, `bootstrap`, by design: a repeat call with
the same words completes a claim that failed after the store). The oracle does
not count a loser's key as partial state. The accept_contract contenders' keys
are stored by the driver itself.

Run it alone: `./tests/run claim-race`.
