# messaging-ops — participants mail each other through the messaging ops

The admin mints participants with `messaging.create_identity`. Each
participant, authenticated over apphost with its own token, reads and writes
its own mailbox through the `messaging.*` ops. The node serves no MCP.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — mint ada, bob and cleo; ada and bob exchange mail
  through `messaging.send_message`, `messaging.list_messages`,
  `messaging.read_messages`, `messaging.wait` and `messaging.archive`; before
  ada reads bob's answer, cleo lists and reads ada's mailbox, and bob and the
  node name it too; ada writes to cleo; an app identity and the node itself
  try the mail ops; `messaging.delete_identity` deletes bob, and ada writes to
  him once more.
- **oracle** `verify.py` — the MCP port was closed and the node's log shows
  no MCP server, the authority was asked
  the sender's and the recipient's side of each message and each reader of
  ada's mailbox, the exchange happened, cleo read ada's mailbox and changed
  none of it while bob and the node were refused, every caller without a
  hosted mailbox was refused, bob's tokens stopped authenticating, and the
  node's own records agree.

## Why the node serves no MCP

mod/messaging owns the mailbox, and mod/mcp is one client of it. `nomcp1` is
in `lib/nodeconfig.py` `WITHOUT_MCP`, so its `mcp.yaml` names an empty
`bind_mcp`: mod/mcp loads and its server never listens. The driver probes the
port reserved for it, and the oracle requires the probe refused, so a node that
listened anyway fails the test rather than passing it for the wrong reason.
A node that ignored the empty `bind_mcp` would listen on mod/mcp's default
address and not on the reserved port, so the oracle also requires the node's
`astrald.log` to hold no `mcp server:` line. messaging's own verbose
`created participant` line is the control: it proves verbose lines reach the
log.

`messaging.*` reached over MCP is `mcp-origin`'s question, on a node that
serves MCP.

## Why the driver serves an authority

The node holds no reachability of its own. A message leaves only when
`mod.messaging.send_action` is granted to its sender, and is stored only when
`mod.messaging.receive_action` is granted to its recipient. The driver's
authority admits ada and bob to each other in both directions, lets cleo read
ada's mailbox, and grants nothing else. cleo is hosted like them, so ada's
refused send to cleo is the authority's refusal and not the node's.

The node asks no authority whether it hosts a mailbox. Each participant signs a
hosting contract naming this node, and auth's chain walk answers
`mod.messaging.host_mailbox_action` from it. The oracle finds exactly one such
contract per participant in the node's `local` repository, separate from the
participant's relay contract, and none for the app identity.

## Why cleo reads ada's mailbox

A caller that names another identity's mailbox makes a delegated read, and the
node asks the authority `mod.messaging.read_mailbox_action` with the caller as
actor and the named mailbox as `MailboxID`. The authority grants cleo ada's
mailbox and nobody else anything. cleo lists ada's inbox and outbox by ada's
alias and reads ada's question with bob's answer under `children` `full`,
naming ada's identity.

The read happens while bob's answer is unread in ada's inbox and uncollected
in bob's outbox. A delegated read never stamps, so ada's lists and bob's row
read the same after it as before. ada then reads the answer herself, and bob's
row is stamped collected: the unchanged rows are the delegation's, not a read
that stamps nothing.

bob corresponds with ada and is not granted her mailbox. His listing is
rejected. His read is accepted, because `messaging.read_messages` learns the
mailbox from its request, and ends with no answer: a refused delegated read
carries no bytes. The node's own identity is
never a reader, so the node's listing is rejected before the authority is
asked anything.

## Why bob is the one deleted

A send to a deleted participant the authority still admits passes the sender's
side and reaches routing. The node then finds no mailbox to deliver to, answers
`delivery failed`, and never asks bob's side. The oracle checks both: the
authority was asked `mod.messaging.receive_action` for bob once, for the
message before the deletion. A deleted participant the authority refuses
would read the same with the mailbox still in place.

The deletion is a withdrawal. bob's hosting contract stays in the node's
repository, and the oracle requires it there.

## Why an app identity and the node are refused

A mail op on the caller's own mailbox serves only a caller whose mailbox this
node hosts. `apphost.register` mints an identity with a token and no mailbox;
the node's own token authenticates as the node, which never hosts a mailbox.
The app's listing and send and the node's listing are rejected before the op
accepts, and the oracle requires `QueryRejected` for each, so a failure that
never reached the guard does not count as the refusal.

The app's read of its own mailbox is accepted, because
`messaging.read_messages` learns the mailbox from its request. The op then
answers `not a messaging participant`, and the oracle requires those words: the
mailbox is the caller's own, so the refusal names its reason, and a caller
tells it apart from a node that went silent.
