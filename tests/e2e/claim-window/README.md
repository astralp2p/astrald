# claim-window — an unclaimed node signs the claim contract and nothing near it

Node setup asks `auth.sign_contract` for the user→node contract as node1, a
caller the signer rule refuses on both parties. While node1 is unclaimed the
guard admits that one contract: issued to node1 by an identity other than
node1 that never issued node1 a relay contract or a node contract, carrying
exactly the permits `user.new_node_contract` writes, expiring no later than
its default validity allows, asked by node1's own session.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — confirm node1 is unclaimed, register an app, then
  ask as an anonymous session for the claim contract of a stranger's key, the
  claim issued by node1, the claim issued by the app, the claim with an extra
  sudo permit, and the claim valid for a century. As the app, ask for the
  stranger's claim. Record what came back and whether node1 is still unclaimed.
- **oracle** `verify.py` — the stranger's claim passed the guard and failed at
  signing, the five near misses were refused by the guard, and node1 is
  unclaimed.

## What this proves that the unit tests do not

The unit tests hold the exception with a stand-in claim state and a stand-in
signer rule. This drives the real user module's claim state before any
contract exists, the relay contract `apphost.register` indexes for the app, and
an anonymous apphost session that `core.Router` gives node1's identity.

## Why the issuer is a stranger

The guard and the signing are separate steps. The stranger's key is the
secp256k1 generator point, whose private key node1 does not hold, so the claim
contract fails at signing (`sign as issuer: unsupported`) once the guard
admits it, and node1 stays unclaimed for the tests after this one.

## Why the driver checks the claim state first

The window exists only on an unclaimed node, and the runner's states name no
such state. A selection that pulls in a two-node prereq runs this test after
`bootstrap-user-software-key`, where every probe reads as a broken guard. The
driver fails there as a driver error, so the report names the order and not
astrald. Run it alone or in `main.suite` order.
