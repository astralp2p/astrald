# distribution-gate-setup — a node under the shipped default gate is set up the way node-setup.md says

Every other claim test runs under the harness's E2E gate, which opens a `fresh`
node to every op and lets a session without a token claim. This test starts
node1 under the gate and the app register policy a released astrald ships, and
sets the node up as `node-setup.md` describes: a session with no token appoints
the manager, and the manager, with its own token, claims the node for a User
from pinned words. The gate's lists run end to end for the first time.

- **env** node · **start** `null` · **saves** — · **destroys** its world: it
  claims node1
- **driver** `script.py` — read `user.info` with no token. At each state ask
  the callers the gate must keep out for the ops it keeps from them, and record
  every answer: with no token on the `fresh` node (`user.bootstrap`,
  `user.derive_identity`, `user.join`, `apphost.register`,
  `apphost.list_tokens`) and with the node's token; with no token, the node's
  token and the manager's, on the `unclaimed` node (`sys.register_manager`,
  `apphost.list_tokens`, and for no token the setup ops). Appoint the manager
  with no token, read `user.info`, preview the User as the manager with
  `user.derive_identity`, claim as the manager with `user.bootstrap` and the same
  words, and read `user.info` with the User's token.
- **oracle** `verify.py` — the claim made the User the pinned identity those
  words derive; the manager is an identity of its own; `user.info` read
  `fresh`, `unclaimed`, `claimed`; every recorded probe was denied, with the
  exact `Denied: <op>…: apphost error: denied` the script reads; on the claimed
  node the User's token authenticates as the User and `user.info` names the
  manager and the contract `User → node1`; a second `user.bootstrap` answers
  `node already claimed` and changes nothing; with no token, `user.info`
  answers `claimed`, `sys.register_manager` answers `node is not fresh`,
  `apphost.register` is declined by the app register policy, `.spec` answers,
  and `user.bootstrap`, `user.derive_identity`, `user.join`,
  `apphost.list_tokens` and `shell.spec` are denied; the node's own token now
  lists tokens, so the gate has opened to the `claimed` list.

## What this proves that the unit tests do not

`mod/apphost/src/config_gate_test.go` holds the shipped lists against the
distribution's `apphost.yaml`, and `gate_test.go` holds the gate with stand-in
modules. This runs a real node with no gate configuration at all, through the
real sys, user and apphost modules: each denial is the gate's answer to a real
session, and the sequence the docs tell a person to follow reaches `claimed`.

## How the shipped gate is started

`test.toml` overrides `apphost.gate` and `apphost.policies` with empty tables.
The harness writes the E2E gate and `app_register: accept_all` under those keys,
and an empty table replaces them with nothing. astrald loads a config over the
defaults in `mod/apphost/src/config.go` (`defaultConfig`), so the lists are the
code's, not a copy in the test; `selftest/test_nodeconfig.py` holds the
override's shape. The harness's token stays: the oracle and the readiness check
reach the node with it. Readiness asks `.spec`, which every state's list
admits, and not `shell.spec`, which only a claimed node's does
(`lib/localnode.py`, `READY_QUERY`).

## Why the words are pinned

`abandon` 23 times and `art` is a valid 24-word BIP-39 phrase. The User it
derives along `m/1791'/0'/0'/0/0` was computed outside astrald, in plain
Python (PBKDF2-HMAC-SHA512 and BIP-32), and the oracle holds the claim to it.
The same script derives the other pinned User in `import-user-software-key`.

## Why node1, and a world of its own

An operator reaches node1 only (`tests/lib/agent.py`), and this flow is one an
operator can drive (`prompt.md`, not yet in `agent.suite`). It claims node1, so
like `import-user-software-key` it destroys its world. Under an operator the
facts are the five the manifest declares; `gate_probes` holds the two
token-less `user.bootstrap` answers it is asked for, and the script's fuller
matrix is judged by the same rules.

## Why no probe claims the node

A caller the gate admits would change the node: the node's token and the
manager may call `user.bootstrap` on an `unclaimed` node. The script therefore
probes only the ops each caller is kept from, and `unclaimed/node` and
`unclaimed/manager` hold the two it keeps from both. A gate that admitted
`user.bootstrap` to a session with no token would claim the node during the
probes, and the test fails there.
