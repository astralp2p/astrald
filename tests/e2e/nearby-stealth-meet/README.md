# nearby-stealth-meet

Two nodes of one User that hold no contract of each other meet on the LAN in
stealth and link, with nothing set up by hand: no scan, no endpoint, no alias,
no introduction.

- **start** `nearby-adopt`: `node1` and `node2` are the User's, both
  broadcasting stealth; the reflector is unowned.
- **roster** `node1`, `node2`, `reflector`.
- **driver** `script.py`: claims the reflector with the User's words through a
  manager, as `introduce-node` claims `replacement`. While every node's log is
  recorded, it waits until `node1` and the reflector list each other linked
  and `node2` and the reflector list each other.
- **oracle** `verify.py`: the claim is the User's; right after it the
  reflector listed itself alone and neither peer listed it; the pair met; a
  node logged `introduced to nearby peer` for the other; `node1` and `node2`
  each logged a contact step for the reflector; the entries the nodes hold of
  each other carry a stealth hint and no public profile.

`node1` and `node2` hold the reflector, unowned, as a verified visible node
under the same IP when it is claimed. nearby emits `PeerSeen` when the user of
a cached node changes, so the claim alone reaches their user module, and the
oracle requires that each acted on it.
