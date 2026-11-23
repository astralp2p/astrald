# nearby-join

A fresh node with a manager joins a User's swarm with `user.join -node <swarm
node>` alone: the swarm node broadcasts in Stealth on the same LAN, nothing
gives the new node an endpoint, and nothing runs on the swarm node.

- **start** `nearby-adopt`: `node1` holds the User's key and a contract, and
  broadcasts in Stealth; the reflector is fresh. **roster** `node1`,
  `reflector` · a VM world (`ops = ["noop"]`): nearby needs a LAN, and a local
  world has none. The test claims the reflector, which `nearby-stealth-meet`
  needs unowned, so it runs in a world of its own.
- **driver** `script.py`: appoints a manager on the reflector, with an
  `admin_network` grant, and reads the reflector's state and the endpoints the
  reflector resolves for `node1`. When the reflector still holds a Visible
  entry of `node1` cached before `node1`'s claim, the driver waits until it
  ends, at most 310 s: the entry lives 5 min, resolves `node1`'s LAN endpoints
  and makes the reflector drop `node1`'s Stealth statuses from the same source
  (astral-docs `protocols/nearby/README.md`, steps 4 and 7). The first netsim
  pass (2026-11-23) met this: `nearby-adopt`'s VM restarts brought the
  reflector up before `node1` was claimed. As the manager it calls `user.join` with
  `node1`'s identity and records the answer and how long it took. It then
  reads the reflector's `user.info` and `node1`'s stored nearby mode.
- **oracle** `verify.py`: before the join the reflector was `unclaimed` and
  resolved no endpoint for `node1`, and `node1` stores no mode other than
  Stealth; the join answered a contract the User issued to the reflector,
  within the 3 s route wait plus the admission; the reflector is `claimed` by
  the User.

What makes the join possible: while `user.join` runs on a node without a
user, the user module recognises the stealth hint of the requested node by
its identity, nearby verifies the status proof under that node's key, and the
verified entry resolves `tcp :1791` (`mod/user/src/status_recognizer.go`,
`mod/user/src/op_join.go`). `user.join` scans first, and `node1` answers
within its 2 s reply window.

Unit tests hold the recognizer, the wait and nearby's checks apart with
stand-in modules; this drives them through real nodes on a LAN.
