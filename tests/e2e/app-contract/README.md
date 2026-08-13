# app-contract

A contract is what makes an app addressable: an app no contract names is
reachable by nobody, and signing one makes the same app answer — from its own
node and from a sibling alike, by `apphost.install_app` and by
`apphost.new_app_contract` + `apphost.sign_app_contract` both.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — two apps serving throughout; three acts, each asking
  from node1 and from node2.
- **oracle** `verify.py` — act 1's objects must be absent, acts 2 and 3's
  present, judged by content-addressed presence in node1's repo.

Three ops provision that contract and no test called any of them:
`apphost.install_app`, `apphost.new_app_contract`, `apphost.sign_app_contract`.

## The peer half works. This test is red on one act, and it is a question

From a sibling the contract does exactly what it should: with none, node2 gets
nothing; `install_app` makes the app answer within about half a second, and
`new_app_contract` + `sign_app_contract` likewise. The signed contract reaches
node2 within milliseconds of the push and is indexed there with its
`mod.nodes.relay_for_action` permit — issuer the app, subject the node.

One act is red at `d43def01`:

    no-contract.host: node1 holds data1ni7heqy58tr…, and should not

**A contract is not required on the app's own node.** Before any contract
existed, node1's own query to the app identity reached the app and was
answered. A handler is matched on the inbound query's target alone
(`mod/apphost/src/query_router.go:17`) with no contract consulted, so
`apphost.register_handler` grants local reach by itself;
`query_preprocessor.go` reads contracts to add a **relay**, which is about
reaching an app elsewhere.

Whether that is a divergence depends on how absolutely one reads
`topics/app-routing.md`'s "the Contract is what makes an App addressable". The
same document's *Answering* section describes astrald's behaviour with no
contract condition at all. So this is a question for the operator before it is
a defect, tracked as *astrald: an app with no contract is reachable on the node
that hosts it*, and the test asserts the specified model until it is settled.

## The peer must ask as the User

Not as node2 itself, and getting this wrong is what made the first draft look
like a much larger defect than it was. Both peer acts failed, and the test
briefly claimed the contract never travelled.

It travels. A query issued under node2's **node** identity takes `mux.go:98`'s
plain-frame branch — a `frames.Query` carries no target, and the receiver
substitutes itself — so the relay hop arrives at node1 addressed to node1 and
is refused in microseconds. Under the **User** identity it is wrapped as a
`RelayQuery` naming caller and target, and it arrives.

That matches the tree's standing convention: `read-remote-peer` states the rule
and every cross-node test follows it. It also matches the mechanism — relay
authorization is written for the user (`mod/user/src/authorizers.go:40`) and an
app's reach is defined over one User's own nodes. An access token is node-local,
so node1's `user_token` does not authenticate on node2 and the peer mints its
own for the same identity.

## Why it asks twice

A contract is supposed to do two separable things: make an app addressable on
the node hosting it, and — through the push to the local user swarm that
signing performs — make it addressable from a sibling holding no key of its
own. Asking only from the host would have called the mechanism proven while
half of it never ran — and the two halves do not agree, which is the whole
finding.

## Why act 1 is judged by an absent object

An unroutable query and a query the app never answered look identical from the
caller's side. A test judging act 1 by the shape of its error would pass on a
node where nothing worked at all. Every (act, asker) pair sends its own
payload, so each reach leaves its own content-addressed object, and act 1 is
judged by objects that must be **absent** while the later acts leave theirs
present.

The apps serve throughout all three acts. Handlers, tokens and queries are
identical in each; the contract is the only variable, which is what makes the
before-and-after a control rather than a coincidence.

## Minting an app identity

`secp256k1.new` hands back a key the node does not keep, and a node that does
not hold the key cannot sign a contract for it. So an app identity is minted
the way `import-user-software-key` mints the User's: derive a key from a seed,
`objects.store` it so crypto indexes it as a signer, and take the identity from
its public key. The mnemonic is that test's, at different derivation paths — a
valid BIP-39 phrase is a checksummed thing, not a sentence anyone can make up.
