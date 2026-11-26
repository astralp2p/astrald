# nearby-adopt-ports

An unowned node that listens off the default ports is admitted through nearby
alone: its Visible status carries its listen ports, and nothing gives the
admitting node an endpoint or an alias.

- **roster** `node1`, `node2` · a VM world (`ops = ["noop"]`): nearby needs a
  LAN, and a local world has none. `node2` listens on `tcp 1800` and
  `kcp 1810` (`[config.node2.tcp]`, `[config.node2.kcp]`); `node1` listens
  on the defaults. Both ports move, so a dial to a default port reaches
  nothing.
- **driver** `script.py`: claims `node1` for a User with generated words,
  scans from `node1`, and waits for `node1`'s `nearby.list` to carry a status
  of `node2` newer than the scan. It reads `node2`'s alias from that status's
  public profile, resolves `.<alias>` and `node2`'s endpoints on `node1`,
  lists `node1`'s links to `node2`, links `node1` as the User to the identity
  `.<alias>` resolved to, and lists the links again. `node2` then joins
  through `node1` at `privileged_member` (`user.join`), which the User
  confirms on `node1` (`user.confirm_join`).
- **oracle** `verify.py`: the status names `node2` and no user, carries the
  `adoptable` flag, one `mod.nearby.listen_ports` of `tcp 1800` and
  `kcp 1810` after the profile, no stealth hint and no sealed ports, and ends
  with its proof; `.<alias>` resolved to `node2`; the endpoints `node1` found
  for `node2` are exactly `tcp :1800` and `kcp :1810` at `node2`'s LAN
  address; `node1` held no link to `node2` before it linked, and right
  after every such link is one `node1` dialed at those endpoints; the
  membership is the User's and names `node2`, `node2` holds it, and
  each node lists the other linked.

The links right after `node1` linked are all `node1`'s: `node2` is unowned and
`node1` broadcasts Stealth, so `node2` cannot identify `node1` and has no
endpoint for it.

Ticket: [Carry listen ports in nearby statuses, sealed in Stealth mode](https://wiki.satforge.dev/doc/carry-listen-ports-in-nearby-statuses-sealed-in-stealth-mode-g6pAQZTYlY),
acceptance criterion 8.
