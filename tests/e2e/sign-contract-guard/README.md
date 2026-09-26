# sign-contract-guard — auth.sign_contract signs only as parties the caller may act as

`auth.sign_contract` signs a contract as its issuer and as its subject with keys
node1 holds. Each party passes the signer rule `crypto.sign_hash` applies: the
caller's own key, never the node's key as the node itself, or a key the caller
may sudo to. A query of any origin other than local is rejected first.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — as the User, register an app, sign the User's own
  contract and ask for one naming the app as subject. As the app, ask for a
  sudo contract in the User's name as issuer and one naming the User as
  subject. As node1's token, ask for the claim contract on the claimed node.
  As an anonymous session, ask for a contract in node1's own name. As the User
  on node2, ask node1 over the link for the User's own contract. As node1's
  token, clear node1's claim from the tree, ask for a stranger's claim and the
  User's claim, and put the claim back. Record what came back.
- **oracle** `verify.py` — the control signed as both parties, each local probe
  was refused by the guard naming the party, the link probe was rejected with
  code 1, the stranger's claim passed the guard with the claim cleared while
  the User's claim was refused, node1 answers as the User's node again, and the
  app still cannot sign a text under the User's key.

## What this proves that the unit tests do not

The unit tests hold the guard with a stand-in signer rule and a stand-in claim
state. This drives the real ones: `mod/crypto`'s `AuthorizeSigner` over the
real sudo chain, the user module's claim state on a claimed node, apphost
sessions that arrive as the User, as an app and as node1, and a relay query
that `mod/nodes` stamps with `network` origin.

## Why the control call matters

A node that signs nothing refuses every probe. The User signing a contract
naming only the User, on the same node in the same run, is checked first.

## Why a refusal must name its party

A refusal and a missing key leave the caller holding the same nothing. The
guard's refusal reads `authorize issuer:` or `authorize subject:`; a missing
key reads `sign as issuer:`, which setup clients retry. The oracle requires the
first and rejects the second, so a probe that reached a key and failed there
does not pass.

## Why the User over the link

The User signs that same contract on node1 locally, so the origin is the only
difference between the control and the probe. node2 mints its own User token,
because `user_token` authenticates on node1 only.

## Why the oracle signs as the User itself

The driver reports that the forged sudo contract was refused. Whether the app
nevertheless acts as the User is a separate claim. A sudo contract from the
User to the app, signed and indexed, would let the app sign under the User's
key through `crypto.sign_text`; the oracle checks that it still cannot.

## Why the driver clears the claim from the tree

`tree.set` on `/mod/user/config/active_contract` needs only
`mod.auth.configure_node_state_action`. node1's token holds it, and so do
node1's anonymous session and an app registered with that grant permit.
Cleared, node1 reads as unclaimed and the claim exception opens. The User's
node contract is indexed, so the exception still refuses to sign the User's
claim again. The stranger's claim is the control: it passes the guard and fails
only at signing, so the exception was open when the User's claim was refused.

The driver puts the claim and the nearby mode back, because `expel-node` runs
on this state next. A module that loses a startup race in astral-go's
`tree.Query` binds its value one level up, at `/user/config` rather than
`/mod/user/config`, so the driver reads the bound path before it writes.
