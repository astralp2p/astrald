# nearby-stealth-meet-ports

Two nodes of one User that listen off the default ports, freshly claimed and
holding no endpoint of each other, link through nearby within two status
periods: each Stealth status carries its node's listen ports sealed under the
node identity, and the sibling that recognises the node opens them.

- **roster** `node1`, `node2` · a VM world (`ops = ["noop"]`): nearby needs a
  LAN, and a local world has none. `node1` listens on `tcp 1800` and
  `kcp 1810`, `node2` on `tcp 1801` and `kcp 1811` (`[config.<node>.tcp]`,
  `[config.<node>.kcp]`), so a dial to a default port reaches nothing.
- **driver** `script.py`: claims `node1` with `user.bootstrap` and generated
  words, and `node2` with `user.bootstrap` and the same words; both then
  broadcast Stealth. While both logs are recorded, it records both nodes'
  links to each other just before `node2`'s claim, then polls both nodes' links
  every 2 s, for up to 240 s from `node2`'s claim, until they share a link. It
  records each node's nearby entry of the other, the stored modes, the
  endpoints each resolves for the other, and the links with their endpoints.
- **oracle** `verify.py`: one User claimed both; neither held a link to the
  other before `node2`'s claim (the claim broadcasts `node2`'s status at once,
  so they may link before it returns); they share a link within 200 s of that claim (two
  status intervals of at most 95 s, and 10 s for the dial); every link a node
  dialed went to the other's LAN address on its carried `tcp` or `kcp` port; a
  node logged `introduced to nearby peer` or `sibling … is nearby at` for the
  other; each entry carries a stealth hint and one `mod.nearby.sealed_ports`,
  no public profile and no `mod.nearby.listen_ports`; no node stores a mode
  other than Stealth; each node resolves the other at its carried ports and
  at neither `1791` nor `1792`.

Nothing gives either node an endpoint, an alias or the other's contract, and
the driver does not scan. Before `node2`'s claim each node may cache the other
as an unowned Visible node; the Stealth status that follows the claim replaces
that entry, so the entries, endpoints and links the oracle reads come from the
sealed ports.

Ticket: [Carry listen ports in nearby statuses, sealed in Stealth mode](https://wiki.satforge.dev/doc/carry-listen-ports-in-nearby-statuses-sealed-in-stealth-mode-g6pAQZTYlY),
acceptance criterion 7.
