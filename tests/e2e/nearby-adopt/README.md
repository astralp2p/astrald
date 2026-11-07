# nearby-adopt

An unowned node on the LAN is listed by a User's node and adopted by the alias
its nearby status carries: nothing gives either node an endpoint or an alias.

- **roster** `node1`, `node2` · a VM world (`ops = ["noop"]`): nearby needs a
  LAN, and a local world has none.
- **driver** `script.py`: claims `node1` for a User with generated words,
  scans from `node1`, and waits for `node1`'s `nearby.list` to carry a status
  of `node2` newer than the scan. It reads `node2`'s alias from that status's
  public profile, resolves `.<alias>` and `node2`'s endpoints on `node1`, and
  as the User adopts `.<alias>`.
- **oracle** `verify.py`: the status names `node2` and no user, carries the
  `adoptable` flag and no stealth hint, and ends with its proof; `.<alias>`
  resolved to `node2`; every endpoint `node1` found for `node2` sits at
  `node2`'s LAN address, `tcp :1791` and `kcp :1792` among them; the adoption
  contract is the User's and names `node2`, `node2` holds it, and each node
  lists the other linked.

The endpoints are read before the adoption, when nearby is the only place
`node1` could have found them; after it, the link adds endpoints of its own.

The User's words are a fact because `nearby-stealth-meet` claims the
reflector with them.
