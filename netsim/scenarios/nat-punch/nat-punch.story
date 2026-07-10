# nat-punch.story — two NAT'd peers hole-punch to a direct kcp link (sibling of tor-link).
#
# Both nodes get Tor WHILE the LAN link is still live (so their onions publish and sync),
# then each enters its own symmetric true-masquerade NAT (astrald in netns priv + port-
# preserving SNAT to 198.51.100.<oct>, severing the direct 10.77 path). A public reflector
# arms each peer's `nat` module by reflecting its public endpoint back. Tor is relocated
# INTO the netns (with WAN egress) so the pair can signal over Tor. Then node1 triggers the
# NAT hole-punch to node2 -> a direct kcp link on BOTH peers.
#
# Signaling is over Tor (source-verified: the tcp-only Basic strategy can't form for two
# symmetric NATs, and the punch client sets no relay hint), so configure-nat-tor is required.
#
# start: two-nodes   save: two-nodes-nat
#   netsim story --stage two-nodes --save two-nodes-nat netsim/scenarios/nat-punch/nat-punch.story
add-vm --hostname reflector
install-astrald --vm reflector
enable-tor --vm node1 --vm node2
enter-nat --vm node1 --vm node2
configure-nat-tor --vm node1 --vm node2
add-reflector --reflector reflector --vm node1 --vm node2
punch-nat --vm node1 --peer node2
# NOTE order: configure-nat-tor (which RESTARTS astrald) must run BEFORE add-reflector.
# add-reflector arms `nat` via an in-memory reflected endpoint; an astrald restart after
# it would wipe that endpoint and disarm nat -> the punch aborts "does not support NAT
# traversal". So arm LAST, after the final restart.
