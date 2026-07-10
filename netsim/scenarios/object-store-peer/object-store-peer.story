# object-store-peer.story — node1 stores an object ON the peer (node2) and reads it back.
# start: two-nodes   save: two-nodes-data-peer
#   netsim story --stage two-nodes --save two-nodes-data-peer netsim/scenarios/object-store-peer/object-store-peer.story
object-store --target node2
