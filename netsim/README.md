# netsim scenarios for astrald

Test scaffolding that drives `netsim` to build and run `astrald` on a simulated
LAN. It contains no astrald Go source and modifies none.

`netsim` boots Ubuntu 26.04 cloud-image VMs on `10.77.0.0/24` with per-VM NAT. A
*task* is a host-side script that configures the VMs. A *story* runs a list of
tasks in one simulation and saves a named *stage*. `lab.story` builds the
`astrald-lab` stage: two nodes running astrald, with a Qwen Code operator on
`node1`.

## Layout

```
netsim/
  tasks/                               # each task: run.sh (+ verify.sh / verify.py) + README.md
    install-astrald/                   # build + run astrald as a service on each node
    configure-astral-agent/            # install the astral-agent skill into the qwen operator
    bootstrap-user-software-key/       # make node1 a User node, new key            -> one-node
    import-user-software-key/          # make node1 a User node, existing mnemonic  -> one-node
    adopt-node/                        # adopt node2 into swarm + register node aliases -> two-nodes
    object-store/                      # node1 stores an object (--target localnode|node2) -> two-nodes-data[-peer]
    read-remote-object/                # node1's agent reads node2's object over astral (used by read-remote-peer)
    expel-node/                        # node1 (User) permanently bans node2 from the swarm -> two-nodes-expel
    _lib/                              # shared verify library (astralapi.py) + astral-py submodule
  scenarios/                           # one dir per scenario: <name>.story + README.md (plain-words)
    lab/                               # null           -> astrald-lab           (fixture)
    bootstrap-user-software-key/       # astrald-lab    -> one-node              (fixture)
    import-user-software-key/          # astrald-lab    -> one-node  (alt.)
    adopt-node/                        # one-node       -> two-nodes             (fixture)
    object-store/                      # two-nodes      -> two-nodes-data        (store on node1)
    object-store-peer/                 # two-nodes      -> two-nodes-data-peer   (store on node2)
    read-remote-peer/                  # two-nodes      -> two-nodes-peer-read   (store on node2, then read it)
    expel-node/                        # two-nodes      -> two-nodes-expel
    tor-link/                          # two-nodes      -> two-nodes-tor         (re-link over Tor)
    nat-punch/                         # two-nodes      -> two-nodes-nat         (NAT hole-punch)
  link.sh                          # register tasks with netsim (idempotent; re-run anytime)
  README.md
```

## Registering tasks

`netsim` discovers tasks only under `~/.local/share/netsim/tasks/`. `link.sh`
symlinks every task under `tasks/` — each folder containing a `run.sh` — there.
It is idempotent; re-run it after adding a task. The symlinks leave netsim's
shipped builtins intact.

```sh
./netsim/link.sh
netsim tasks        # confirm: install-astrald is listed as a user task
```

## Verifier library

The `verify.py` oracles share `tasks/_lib/astralapi.py`, which reaches each
VM's apphost through the **astral-py** client vendored as a submodule at
`tasks/_lib/astral-py`. Initialize it once per worktree (`workon.sh` does not
`--recurse`); a missing submodule fails with a loud `ImportError`:

```sh
git submodule update --init netsim/tasks/_lib/astral-py
```

The verifiers fall back to the Go `astral-query` CLI for any op the client can't
serve, but the submodule must be present for `verify.py` to import.

## Lab

`lab.story` builds the full lab in one simulation: two nodes running astrald and
a Qwen Code operator on `node1`, equipped with the `astral-agent` skill.

```
# lab.story — the astrald lab, built in one netsim simulation.
# Result: a single stage with two nodes running astrald and a Qwen Code
# operator on node1, equipped with the astral-agent skill.
add-vm --hostname node1
add-vm --hostname node2
install-astrald
install-qwen-code --vm node1 --create-user
configure-astral-agent --vm node1
```

A story is a plain-text file with one `task [args...]` per line, shell-style
quoting, and `#` for full-line or trailing comments. `netsim story` boots one
simulation, runs the listed tasks in order in the same VMs, and saves a single
stage at the end. It stops at the first failing task. Order is significant:

* `add-vm --hostname node1` and `add-vm --hostname node2` use the `add-vm`
  builtin; they create the two plain Ubuntu VMs on the LAN.
* `install-astrald` is the [custom task](tasks/install-astrald/README.md); with no
  `--vm` it installs astrald on every running VM, so on both nodes. It runs
  `run.sh` then `verify.sh` and fails the story unless astrald builds, starts, and
  answers `astral-query localnode:.spec` on every node. The service is left
  enabled and running, so the stage snapshots a live node that resumes
  already-running on restore.
* `install-qwen-code --vm node1 --create-user` uses the `install-qwen-code`
  builtin; it installs the Qwen Code CLI on `node1` and points it at the
  inference endpoint. The builtin installs for user `tester`, which does not
  exist on a fresh cloud image, so `--create-user` is required. `node2` stays a
  plain astrald peer.
* `configure-astral-agent --vm node1` is a [custom task](tasks/configure-astral-agent/README.md);
  it installs the `astral-agent` skill into the Qwen Code operator so it can drive
  astrald from the skill's knowledge. The host must have `SATFORGE_SKILLS_DEPLOY_KEY`
  set (a deploy key for the private skills repo) — see its README.

Both VMs must exist and run before `install-astrald`, astrald must be present
before the Qwen Code operator is layered on `node1`, and the operator must exist
before its skill is configured.

Register the custom tasks once (see [Registering tasks](#registering-tasks)),
then build the lab:

```sh
./netsim/link.sh
export SATFORGE_SKILLS_DEPLOY_KEY=~/.ssh/satforge_skills_deploy   # see tasks/configure-astral-agent
netsim story --stage null --save astrald-lab netsim/scenarios/lab/lab.story
```

The result is the stage `astrald-lab`: `node1` and `node2` running astrald, with a
Qwen Code operator on `node1` equipped with the `astral-agent` skill. Re-enter it
with `netsim shell --stage astrald-lab`.

## Swarm pipeline

Each post-lab flow is its own scenario under `scenarios/<name>/` — a `<name>.story`
plus a plain-words `README.md` — layered on the previous stage (its `start`/`save`
stages are in the story header and README). Intermediate stages stay reusable, so
you can replay one flow without rebuilding the chain:

```
astrald-lab ─[bootstrap-user-software-key]→ one-node ─[adopt-node]→ two-nodes ─[object-store]→ two-nodes-data
```

```sh
netsim story --stage astrald-lab    --save one-node             netsim/scenarios/bootstrap-user-software-key/bootstrap-user-software-key.story
netsim story --stage one-node       --save two-nodes            netsim/scenarios/adopt-node/adopt-node.story
netsim story --stage two-nodes      --save two-nodes-data       netsim/scenarios/object-store/object-store.story
netsim story --stage two-nodes      --save two-nodes-peer-read  netsim/scenarios/read-remote-peer/read-remote-peer.story
netsim story --stage two-nodes      --save two-nodes-expel      netsim/scenarios/expel-node/expel-node.story
```

`expel-node` is a separate branch off `two-nodes`: the User on node1 permanently
bans node2, so the swarm roster shrinks (node2 drops out of `user.swarm_status`,
lands in `user.list_expelled`, and the link is torn down). It produces its own
`two-nodes-expel` stage rather than feeding the data-object chain.

Each story drives the Qwen operator through its `astral-agent` skill, then runs an
independent `verify.sh`/`verify.py` check — so a story is a pass/fail integration
test for that flow.

## Scope

The lab stands up two astrald nodes, links them into one User Swarm, stores an
object on a node, and reads it from a peer across the swarm. Nodes discover each
other on the shared L2 LAN via UDP 8822 (`ether`/`nearby`).
