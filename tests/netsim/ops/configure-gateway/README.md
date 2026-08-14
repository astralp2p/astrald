# configure-gateway

Makes one VM a gateway and points others at it.

    vm:configure-gateway <client-vm>... --gateway <host>

Bare words are the harness's `--vm` clients; `--gateway` names the gateway VM.

## Why an op and not driver calls

Both halves are config, not ops, and neither is reachable over apphost:

- A node is a gateway only with `gateway.enabled: true` — `canGateway` reads
  nothing else (`mod/gateway/src/module.go`).
- A node registers with one only by listing it under `gateways`. The module
  then schedules `MaintainGatewayConnectionsTask` and registers itself
  (`module.go` `addPersistentGateway`), so registration is astrald's own doing
  and a test waits for it rather than performing it.

Both therefore need a file and a restart, which is a vmop's job.

## Why an explicit endpoint

`getGatewayEndpoint` falls back to `IP.PublicIPCandidates()`, and a 10.77 lab
address is not public — the gateway would answer *no public IP available for
network tcp*. `networks.tcp.endpoint` is the override `deps.go` parses into
`configEndpoints`, and it takes the gateway's own LAN address.

## Why identities come from the environment

`ASTRAL_ID_<vm>` is exported by the harness, which learns every identity at
tunnel time and is the authority on it. A vmop that hunts for an identity by
alias works under the script driver and dies under the agent one — the lesson
`leave-lan` already paid for.

## Ordering

Run it after the roster is up, so every identity is exported. Run it after
`enter-nat` if a client is going behind NAT: the readiness check asks where
astrald actually is, because a NAT'd node's apphost listens on the netns
loopback and a root-namespace `astral-query` reaches nothing.
