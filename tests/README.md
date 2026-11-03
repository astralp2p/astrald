# tests/ — the Astral integration testing system

One command, real daemons, deterministic verdicts:

    ./tests/run                        # the default suite (suites/main.suite)
    ./tests/run adopt-node             # one test, and the tests its world needs first
    ./tests/run --plan main.suite      # what a run would do, with nothing started
    ./tests/run smoke --target attach  # judge a daemon you already run

Design: https://wiki.satforge.dev/doc/integration-testing-system-tAKjyvbcHo ·
vision: https://wiki.satforge.dev/doc/improving-on-e2e-system-2C4ELWFOgg

## Add a test in five minutes

For humans and AI agents alike.

**1. Make the directory** `e2e/<name>/` with four files:

| file | what it is |
|------|------------|
| `test.toml` | the manifest: what the test needs (below) |
| `script.py` | the driver: performs the flow, writes facts with `lib.sessionio.write_facts` |
| `verify.py` | the oracle, the only judge: exits 0 when astrald did the right thing |
| `README.md` | what the test proves, in words |

A driver and an oracle read one file, their session view, through
`lib.sessionio.load()`: `{"run", "nodes", "facts"}`. Each node carries
`endpoint`, `token`, `identity` and the other fields `lib/worlds` lists in
`NODE_FIELDS`, `null` where its world has none. Neither ever learns whether
a node is a process two ports away, a VM behind a tunnel, or your own daemon.

**2. Write the manifest.** Every key but `nodes` is optional:

```toml
nodes      = ["node1", "node2"]     # the roster: names this test's nodes
after      = "adopt-node"           # the test whose world this one continues; absent: a fresh world
destroys   = true                   # only this test's descendants may follow it in its world
ops        = ["enable-tor node1"]   # VM operations (netsim/ops/) run before the driver; imply destroys
local_only = "reads a node root"    # it needs the host, so it never enters a VM world
restart    = ["node1"]              # nodes restarted between driver and oracle (local worlds only)
facts      = ["object_id"]          # the facts its driver writes
timeout    = 180                    # the driver's and the oracle's budget, in seconds (default 120)

[config.node1.objects]              # a module config override for one node, per world
external_registration_sweep_interval = "1s"
```

- **`after`** is the whole ordering model: a test names the test whose
  world it continues, never a state. A new test needs only its files and an
  `after`.
- **`destroys`** marks a test that leaves its world unusable for anything
  but its own descendants: it expels a node, purges a repository, moves a
  node behind NAT, or claims a node another test needs unclaimed.
- **`local_only`** is for a test that reads `mcp_url`, `authority_url`,
  `serves_mcp`, a node `root` or a host path, that serves an app or an
  authority on the host for a node to dial, or that restarts a node.
- **`facts`** is a contract. An oracle may read only facts declared on its
  `after` chain (`selftest/test_shipped.py` checks), and the agent driver
  asks the operator for exactly these.
- **`[config.<node>.<module>]`** writes over the harness's own config for
  that module (`lib/nodeconfig.py`), key by key; a module the harness
  builds nothing for gets the table alone. The config follows the world,
  not the test: the planner merges the overrides of every test in a world,
  a test that sets a key otherwise than its world forks into one of its
  own, and a clash with its own lineage is a plan error. `--plan` lists a
  world's overrides under its steps.
  - Keys the harness owns, like `apphost.tokens` and the listeners' ports,
    are refused. `mcp.bind_mcp` may only be `""`, which turns MCP off:
    `messaging-ops` runs on `[config.nomcp1.mcp] bind_mcp = ""`.
  - A local world writes each module's config as JSON, which astrald's YAML
    loader reads. A VM world pushes the same files into each guest with the
    binaries, before its ops, so a test whose override an op in its world
    would write over forks, and a lineage holding both is refused.
  - `--target attach` refuses an override: it renders nothing.

**3. List it in a suite.** A suite under `suites/` lists tests; its order is
only a preference among siblings. Every test is in some suite —
`selftest/test_shipped.py` fails one that is in none, since nothing else
would ever run it.

**4. Look before you run, then run:**

    ./tests/run --plan my-test     # the worlds it plans into; `*` marks a prereq
    ./tests/run my-test            # run it, with whatever its `after` chain needs first
    ./tests/run --isolate main.suite   # every test in a world of its own
    ./tests/run my-test --keep     # leave the last world up to poke at

`--isolate` is how you tell a real regression from interference: if a test
passes alone and fails in the shared run, a test before it changed its world.

**5. Read the result.** The runner prints a verdict and the path of
`results/<stamp>/report.md`. See [Reading a failure](#reading-a-failure).

## Suites

| suite | what it is | worlds | runs in |
|-------|------------|--------|---------|
| `main.suite` | every local test, the everyday one | local | ~3 min |
| `tor.suite` | two nodes meet on a LAN, one leaves it, Tor holds | VM | ~37 min |
| `nat.suite` | two nodes meet on a LAN, then hole-punch through NAT | VM | not measured |
| `gateway.suite` | a NAT'd node reaches its swarm through a gateway | VM | not measured |
| `agent.suite` | the flows an AI operator can drive (`--driver agent`) | VM | ~1.5 h |

The plans of the shipped suites are pinned under `selftest/golden/`, so a
manifest change that moves a test shows up as a diff someone reads.

## Worlds

The planner (`lib/plan.py`) turns a selection into worlds, and the runner
runs them one after another, each from fresh machines:

- tests that only observe share a world, and run before anything builds;
- a test that builds state continues the world;
- every further builder, and every destroyer but one, gets a fork that
  replays its lineage as prereqs, by script.

A world is one of three kinds:

| kind | the machines | when |
|------|--------------|------|
| local | astrald processes on loopback | the default |
| vm | the lab's VMs on a simulated LAN (`netsim/labs/`) | a test with `ops`, `--vm`, or `--driver agent` |
| attach | the daemon you already run | `--target attach` |

A VM world boots the lab, pushes the astrald and astral-query under test to
every VM, runs ops with their verifiers, and on close pulls each guest's
astrald log into `results/<stamp>/w<N>/logs/`. VM worlds run one at a time.

## Drivers: script and agent

| driver | who | how |
|--------|-----|-----|
| `script` | `script.py` | code, exact and fast |
| `agent` | an AI operator in the lab | reads `prompt.md` and works it out |

Both are judged by the same `verify.py`, and that is the whole point:
**script red means astrald broke; agent red while script is green means the
operator or its prompt broke.**

A test an operator can drive is exactly one that ships a `prompt.md`: the
flow in plain words, the way a person would ask for it. The operator is
Qwen Code, baked into the lab's `node1` with the astral-agent skill, and
`lib/agent.py` drives it:

- the prompt's placeholders fill from the step's view: `{facts.<name>}`,
  `{<node>.identity}` and `{<node>.alias}`, the roster name. An unknown
  one is refused before anything runs, and so is any that would hand the
  operator a token: it must never hold what it is judged with;
- a footer asks for exactly the test's `facts`, in `~/.facts/<test>.json`.
  That file alone is collected, and a declared fact missing from it is a
  `driver` failure;
- the model is `config.toml`'s `[agent] model`, set in the operator once
  per world; `base_url` applies only with a model. The report names the
  model, since a red under one model says nothing about another;
- an operator session gets `agent.TIMEOUT`, 3600 s: one measured session
  took 2404 s.

Under `--driver agent` the selected tests are operator-driven and their
prereqs run by script; `--prereqs agent` hands the prereqs to the operator
too.

## Fast loops

- **`--keep`** leaves the run's last world up: a local world's nodes, or a
  VM world's simulation with its tunnels.
- **`--target attach`** runs a selection against a daemon you already run,
  resolved the way astral-py resolves one (`ASTRAL_ENDPOINT`,
  `ASTRALD_APPHOST_TOKEN` and friends). It is the code-and-debug loop, with
  open eyes: nothing is torn down, and a `destroys` test really changes your
  daemon and says so first. The plan is refused when the selection needs a
  prereq (attach checks a state, it never builds one), a roster names more
  than one node, a test is `local_only`, or the driver is `agent`.
- **`--save` / `--from`** skip rebuilding a VM world:

      ./tests/run adopt-node --vm --save gw     # build it in VMs, save it as gw
      ./tests/run gateway-relay --from gw       # boot gw, run only gateway-relay

  `--save NAME` saves the last world, a VM world whose every step passed, as
  netsim stage NAME. Its world record goes into every guest, root-only, so
  the operator cannot read the tokens it is judged with. `--from NAME` boots
  it, skips the saved history (`=` in `--plan`), and takes its facts from
  the saved record.

Every target but `attach` strips the ambient endpoint and token variables
from the driver environment: a developer's `ASTRALD_APPHOST_TOKEN` would
otherwise answer for a test's anonymous connect.

## Reading a failure

`report.md` names each failure's kind and the phase it stopped at:

| failure kind | means |
|--------------|-------|
| `driver` | the flow never happened — the driver could not perform it |
| `verify` | astrald misbehaved — the flow ran and the oracle rejected it |
| `environment` | the world broke — no verdict about astrald is possible |

| phase | the step that stopped |
|-------|-----------------------|
| `prepare` | bringing the roster up and running its `ops` |
| `driver` | `script.py`, or the operator |
| `restart` | the restart `restart` names |
| `verify` | `verify.py` |
| `alive` | a node died under the test |

A skipped test carries a `reason`: the test on its `after` chain that failed
in its world, the world dying, or the run being interrupted. A run holding a
skip reports INCOMPLETE, never PASS; a failure outranks a skip.

Under `results/<stamp>/`:

- `report.md` for people, `results.json` and `events.jsonl` for machines
  (schema in `lib/results.py`);
- `<test>/`: the test's `session.json` view, `driver.log`, `verify.log` and
  `facts.json`. A test replayed as a prereq in a later world writes
  `<test>.w<N>/`;
- `w<N>/world.json`: the world record — its nodes, the history of what ran,
  and the facts of every test that passed;
- `w<N>/session/`: a local world's node roots and astrald logs;
  `w<N>/logs/`: a VM world's guest journals.

The report is written however the run ends — an exception, Ctrl-C, a world
that failed to close — and `./tests/summary` lists what has passed here
across runs.

## Requirements

Python ≥ 3.11, a Go toolchain, and an astral-py checkout (path in
`config.toml`) — imported directly from its `src/`, since the package has
zero dependencies. The checkout holds the commit `config.toml`
`astral_py.requires` names; `tests/run` refuses an older one before it builds
anything. No venv, no pip. A VM world additionally needs a working
`netsim` on PATH, and `./tests/link.sh` run once per checkout to register
this tree's netsim ops.

The harness's own selftests take a second and touch no daemon:

    cd tests && PYTHONPATH=.:<astral-py>/src python3 -m unittest discover -s selftest -t .
