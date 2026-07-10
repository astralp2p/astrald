# tor-link.story — a node leaves the LAN and links over Tor (scenario 0004).
# Both nodes get system Tor (so astrald's tor module can publish/dial onions); node2
# drops its LAN path to node1 (after node1 is seeded with node2's onion); then node1's
# agent re-establishes the swarm link over Tor.
# start: two-nodes   save: two-nodes-tor
#   netsim story --stage two-nodes --save two-nodes-tor netsim/scenarios/tor-link/tor-link.story
enable-tor    --vm node1 --vm node2
leave-lan     --vm node2 --peer node1
link-over-tor --vm node1 --peer node2
