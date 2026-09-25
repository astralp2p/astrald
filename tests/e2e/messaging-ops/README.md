# messaging-ops — participants mail each other through the messaging ops

The admin mints participants with `messaging.create_identity`. Each
participant, authenticated over apphost with its own token, reads and writes
its own mailbox through the `messaging.*` ops. The node serves no MCP.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — mint ada, bob and cleo; ada and bob exchange mail
  through `messaging.send_message`, `messaging.list_messages`,
  `messaging.read_messages`, `messaging.wait` and `messaging.archive`; ada
  writes to cleo; an app identity and the node itself try the mail ops;
  `messaging.delete_identity` deletes bob, and ada writes to him once more.
- **oracle** `verify.py` — the MCP port was closed and the node's log shows
  no MCP server, the authority was asked
  the sender's and the recipient's side of each message, the exchange
  happened, every caller without a hosted mailbox was rejected, bob's tokens
  stopped authenticating, and the node's own records agree.

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
authority admits ada and bob to each other in both directions and nothing
else. cleo is hosted like them, so ada's refused send to cleo is the
authority's refusal and not the node's.

The node asks no authority whether it hosts a mailbox. Each participant signs a
hosting contract naming this node, and auth's chain walk answers
`mod.messaging.host_mailbox_action` from it. The oracle finds exactly one such
contract per participant in the node's `local` repository, separate from the
participant's relay contract, and none for the app identity.

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

A mail op serves only a caller whose mailbox this node hosts. `apphost.register`
mints an identity with a token and no mailbox; the node's own token
authenticates as the node, which never hosts a mailbox. Both are rejected
before the op accepts, and the oracle requires `QueryRejected` for each, so a
failure that never reached the guard does not count as the refusal.
