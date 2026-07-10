# read-remote-peer.story — store an object on the peer (node2), then node1's agent
# reads it back from the peer over astral.
# start: two-nodes   save: two-nodes-peer-read
#   netsim story --stage two-nodes --save two-nodes-peer-read netsim/scenarios/read-remote-peer/read-remote-peer.story
object-store --target node2
read-remote-object
