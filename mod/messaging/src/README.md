# messaging

messaging hosts mailboxes and carries mail between them. A participant is an
identity with an inbox and an outbox. This node hosts a participant's mailbox
under a hosting contract the participant signs. The protocol is
[messaging](../../../.ai/system/protocols/messaging/README.md).

mod/mcp is one client of this module: its agents are participants, and its mail
tools call the `messaging.Module` methods under the bearer's identity.

## Participants

* `messaging.create_identity` mints a participant: a key this node stores and
  indexes, a relay contract, a hosting contract, an optional alias, an apphost
  access token, and a row in the hosting index.
* The index row is written last. A create that fails at an earlier step leaves
  no served mailbox.
* `mcp.create_agent` mints its agent through `CreateIdentity`.
* `messaging.delete_identity` revokes every apphost token and grant of the
  identity, unsets its alias, and withdraws its mailbox.
* A withdrawal deletes the index row and every message the identity owns in one
  transaction. A correspondent's copy of a message stays.
* A delivery or a send admitted before a withdrawal writes its row before the
  withdrawal, or writes nothing.
* A withdrawal is not a revocation. The relay contract and the hosting contract
  stay valid until they expire, wherever they are held.

## Hosting

* A hosting contract has the participant `U` as issuer and this node `N` as
  subject. The contract carries one permit for
  `mod.messaging.host_mailbox_action`, with delegation `0` and no constraints.
  The contract expires after `hosting_duration`.
* The hosting contract is separate from the relay contract. A relay permit
  never authorizes hosting, and a hosting permit never authorizes relaying.
* The module registers the root rule for `mod.messaging.host_mailbox_action`:
  allow when `MailboxID` is nonzero and equals the actor. The rule is not
  node-local, so auth's chain walk reaches it through `U`'s contract.
* `messaging__mailboxes` is the hosting index: `identity`, `contract_id`,
  `expires_at` and `created_at`. A row with no `contract_id` is pending.
* The index is mirrored into memory at load.
* This node hosts `U` while the index names an unexpired contract for `U` and
  auth authorizes `HostMailboxAction{Actor: N, MailboxID: U}`. The node
  identity is never hosted.
* Every eligibility decision asks the hosting question: the routing of
  `messaging.message` and `messaging.receipt`, every mail operation and every
  mail method, and whether a sender is local when its message is fetched.
* The index authorizes nothing by itself. A contract that no longer authorizes
  stops routing and the mail operations, and the row and the stored mail stay.
* Only contracts this node provisioned are indexed. A hosting contract auth
  indexed from elsewhere is not served until a provisioning path records it.
* Nothing renews a hosting contract.

## Delivery

* A send asks `mod.messaging.send_action` with the sender as actor and the
  recipient as `ToID`. A refusal answers `unknown recipient`, the same words as
  a recipient that does not exist.
* The sending node writes the outbox row, then routes a `messaging.message`
  query from the sender to the recipient on the node's own context.
* A recipient on another node is reached over a link as a relay query. The
  sending node finds the recipient's node through the recipient's relay
  contract, which it must hold.
* The recipient's node asks `mod.messaging.receive_action` with the recipient
  as actor and the sender as `FromID`. A refusal rejects the query with
  `RejectNotAdmitted` (`5`).
* A read of the caller's own mailbox that hands out an inbox body stamps the
  row `read_at` and the sender's outbox row `fetched_at`. The `fetched_at`
  stamp is direct when this node hosts the sender, and otherwise one
  `messaging.receipt` query to the sender carries it.
* A receipt is admitted by the matching outbox row and asks no action.
* Delivery and receipt queries carry no origin. No operation is named `message`
  or `receipt`.
* The module takes a `messaging.message` or `messaging.receipt` query only over
  a link or from its own send path, which marks the query in `Extra`. A query
  with any other provenance is rejected before the hosting check, whatever its
  target. A query an agent's declared tool puts and a query a local app routes
  itself are such queries.

## Operations

* `create_identity`, `identity` and `delete_identity` refuse network and MCP
  origin. `create_identity` and `delete_identity` ask
  `mod.auth.admin_manage_apps_action`, and `identity` asks
  `mod.auth.see_node_state_action`.
* `send_message`, `list_messages`, `read_messages`, `wait` and `archive` refuse
  network and MCP origin.
* `send_message`, `wait` and `archive` refuse a caller whose own mailbox this
  node does not host. `list_messages` and `read_messages` refuse such a caller
  on its own mailbox.
* `send_message`, `list_messages`, `wait` and `archive` reject a refused query
  before they accept it.
* `read_messages` rejects a network or MCP origin, the zero identity and this
  node before it accepts the query. `read_messages` answers
  `not a messaging participant` to a caller whose own mailbox this node does
  not host, once the request arrives.
* `send_message`, `wait` and `archive` act on the caller's own boxes. No
  argument names an owner.
* `list_messages` and `read_messages` act on the caller's own boxes unless they
  name another mailbox — see [Delegated read](#delegated-read).
* `wait` accepts the query before it parks. The park ends when a message
  arrives, when the granted window closes, or when the caller closes the
  channel.

## Delegated read

* `list_messages` takes the argument `mailbox`, a hex identity or an alias. A
  `messaging.read_messages_request` carries `Mailbox`. An absent mailbox, or
  one naming the caller, reads the caller's own mailbox.
* A mailbox naming another identity makes the call a delegated read. The checks
  run in this order: the caller is neither the zero identity nor this node, this
  node hosts the named mailbox, and auth grants
  `ReadMailboxAction{Actor: caller, MailboxID: mailbox}`. The caller needs no
  mailbox on this node.
* The module registers no rule for `mod.messaging.read_mailbox_action`. Another
  registered handler or the external authority the node names for the action
  grants a delegated read. A node with neither refuses every delegated read.
* No contract carries `mod.messaging.read_mailbox_action`. The action refuses
  every permit, constrained or not (astral-go `api/messaging`
  `ReadMailboxAction.ApplyConstraints`), so auth's chain walk passes no
  contract link for it, and auth asks the handlers and the external authority
  about the caller alone. The astral-go `ReadMailboxAction` doc comment holds
  the reason no contract carries the action.
* `addAuthorizers` in `authorizers.go` holds the reason no rule exists and the
  limit of the refusal.
* A refused `list_messages` is rejected before it is accepted.
* `read_messages` learns its mailbox from the request, which arrives after the
  query is accepted. A refused delegated read therefore ends the query with no
  answer.
* A delegated read never stamps. It writes no `read_at` and no
  `receipt_due_at`, sends no receipt, and writes no `fetched_at` on a local
  sender's row. The rule holds for the named messages and for replies under
  `children` `full` alike.
* The `messaging.Module` methods act on the owner's own mailbox. A request that
  names another mailbox is refused. mod/mcp's tools name no mailbox.

## Storage

* `messaging__messages` holds both boxes, one row per owner, box and id. The
  owner is the recipient of an inbox row and the sender of an outbox row.
* `seq` is the cursor the inbox listing and `wait` page by. The outbox and the
  archive are histories, read newest first, and refuse a nonzero `since`.
* On a node that ran mod/mcp's mail, the migration renames `mcp__messages` to
  `messaging__messages` with every row and the `seq` high-water mark, and
  replaces its indexes.
* The migration that creates `messaging__mailboxes` imports every `mcp__agents`
  identity as a pending row.
* Run provisions a hosting contract for every pending row whose key this node
  holds. A row that cannot be provisioned is logged and stays pending and
  unserved.

## Configuration

The config file for the module is `messaging.yaml`. The defaults:

```yaml
hosting_duration: 87600h
token_duration: 8760h
delivery_timeout: 15s
wait_default: 2m
wait_max: 15m
max_payload_bytes: 65536
max_read_bytes: 65536
```

* `hosting_duration` is the validity of a new hosting contract.
* `token_duration` is the validity of a new participant's access token when
  `create_identity` names no `duration`.
* `delivery_timeout` bounds one delivery, one receipt, and the read of either
  on the answering side.
* A wait that names no window is granted `wait_default`. A wait that asks for
  more than `wait_max` is granted `wait_max`. Every answer names the granted
  window beside the time waited.
* `max_payload_bytes` bounds a message body, on the way out and on the way in.
* `max_read_bytes` bounds the message bodies one read answers. A body left out
  for the bound is marked truncated.
