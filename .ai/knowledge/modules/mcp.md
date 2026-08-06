# mod/mcp

Serves MCP (Model Context Protocol) over streamable HTTP so AI agents can use
the astral network. Owns the agent registry (`mcp__agents`), the MCP endpoint
and its bearer-token gate, and the dialog sessions that bridge MCP tool calls
to live astral connections.

## Dependencies

| Module | Why |
|---|---|
| `apphost` | `CreateAccessToken` mints the agent PAT; `AuthenticateToken` resolves bearer tokens; `DeleteAccessToken` revokes on delete |
| `auth` | `SignContract` + `IndexContract` for the node→agent relay contract |
| `crypto` | `AddToIndex` registers the minted agent key for signing |
| `dir` | `SetAlias`/`GetAlias`/`ResolveIdentity`/`DisplayName` for agent aliases and tool targets |
| `objects` | `Store` persists the agent key and signed contract |
| `user` (opt) | `Identity` fills the user fields of `astral-whoami`; nil on an unclaimed node |

## Invariants

* All `mcp.*` ops reject `astral.OriginNetwork`.
* Empty `bind_mcp` disables the MCP endpoint; the module still serves ops.
* Any valid apphost access token authenticates to the MCP endpoint, not only mcp-minted ones.
* Every tool acts as the authenticated identity; tools are bound per HTTP session via the SDK `getServer` callback.
* A session belongs to one agent; tools refuse session handles of other agents.
* One parked `astral-listen` per identity; a second concurrent listen fails.
* `RouteQuery` pops the parked listener atomically — exactly one query wins it — and accepts synchronously via `query.Accept`; the agent replies later through the session.
* An inbound query for a registered agent with no parked listener is accepted and queued for the agent's next `astral-listen`, up to `max_pending` per agent and `pending_ttl` each; unregistered targets and a full queue fall through to other routers immediately.
* `astral-listen` drains the pending queue before parking, and again right after parking to close the enqueue race.
* `astral-query` single-shot auto-detects the response: framed objects when the bytes decode cleanly, plain payload otherwise; `format` forces `raw` or `objects`.
* A session's pump goroutine owns all conn reads; per-session format (`raw` or `objects`) is fixed at creation. Dialog sessions default to `raw` on both sides.
* Sessions expire after an idle `session_ttl`, refreshed on every send/receive; expiry and `astral-send close:true` close the conn.
* A session that loses its listener (timeout race) is closed by the drain or expires via `session_ttl`.
* `delete_agent` revokes the stored token, unsets the alias, drops the row, and closes live listeners/sessions; the signed relay contract stays indexed until it expires.

## Flows

* Create agent: mint key → `Objects.Store` + `Crypto.AddToIndex` → sign + index node→agent relay contract → alias (given or `aliasgen`) → `Apphost.CreateAccessToken` → `mcp__agents` row → send `mcp.Agent`.
* Inbound dialog: caller queries the agent identity → `RouteQuery` pops the parked listener, or queues when none is parked → accept → session pump starts → payload read within `payload_read_window` → session delivered to `astral-listen` → agent answers with `astral-send` / reads more with `astral-receive` → `close:true` ends.
* Outbound dialog: `astral-query` with `session:true` routes as the agent, registers the open conn as a session and returns the handle instead of collecting a response.
