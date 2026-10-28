# two-node-plus-external

The lab every netsim run starts from: **node1 and node2, the User's swarm —
plus `reflector`, a third full node that belongs to no swarm.** Three VMs,
three astralds, two of them adopted.

The name says all three because the third is not scenery. It runs the same
astrald as the others, and what makes it useful is not that it is third but
that it is **outside the swarm**: `nat-punch` needs an unmasqueraded observer
to bounce a public address off, and `gateway-relay` needs a gateway that is a
stranger to the nodes registering with it.

A test opts the external machine in by naming it, exactly as it names any
node — `nodes = ["node1", "node2", "reflector"]`. Most tests do not, and
declare the pair alone. Nothing else distinguishes it: every machine gets a
pushed astrald and a token on every boot, and the roster is the only gate to
a tunnel and a `session.json` entry.

`reflector` is a name from its first use and now under-describes it, since it
also serves as the gateway. Renaming it means editing `lab.story`, whose bytes
are the recipe hash, so every cached stage would be orphaned — worth doing on
its own, not in passing.

- **Chain:** `null` → `e2e-lab-r<hash>`, where `<hash>` is the first 8 hex of
  `lab.story`'s sha256, so an edit to the recipe bakes a new stage
- **Builds:** three VMs at 2048 MiB · astrald on all three · Qwen Code on node1
  · the astral-agent skill linked into it
- **Bake:** the netsim executor bakes it on the first run that finds no stage
  for the current hash:
  `netsim story --stage null --save e2e-lab-r<hash> tests/netsim/labs/two-node-plus-external/lab.story`

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
