# two-node

The lab every netsim run starts from: **node1 and node2, the roster every test
names — plus an off-roster `reflector`** that exists so `nat-punch` has an
unmasqueraded observer to bounce a public address off. Three VMs, two nodes.

The directory is named for the roster, not the machine count, because the
roster is what a manifest declares (`nodes = ["node1", "node2"]`, in every
test that has two). A test never addresses the reflector.

- **Chain:** `null` → `astrald-lab`
- **Builds:** three VMs at 2048 MiB · astrald on all three · Qwen Code on node1
  · the astral-agent skill linked into it
- **Bake:** `netsim story --stage null --save astrald-lab tests/netsim/labs/two-node/lab.story`

astrald is *installed* here and **pushed fresh on every boot** by the netsim
executor, so a stage outlives the commit that filled it. What the bake is for
is everything slow and stable: the VMs, the apt dependencies, the service
unit, the operator and its skill.

2048 MiB is not decoration. `install-astrald` builds astrald inside each
guest, and `modernc.org/sqlite/lib` alone does not fit a 1 GiB default — the
compiler is OOM-killed and the bake dies at the third node.

The ops this recipe calls live in `../../ops/`, beside the ones tests run as
steps: netsim keeps one flat task namespace and cannot tell the two apart, so
neither does the tree. Register them with `tests/link.sh`.
