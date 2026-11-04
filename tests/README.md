# tests/ — the Astral integration testing system

One command, real daemons, deterministic verdicts:

    ./tests/run                        # the default suite (suites/main.suite)
    ./tests/run adopt-node             # one test, and the tests its world needs first
    ./tests/run --plan main.suite      # what a run would do, with nothing started
    ./tests/run smoke --target attach  # judge a daemon you already run

The default suite needs no netsim and runs on Linux or macOS; see
[Without netsim](#without-netsim).

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
after      = "adopt-node"           # the test whose world this one continues; absent: a root test
destroys   = true                   # only this test's descendants may follow it in its world
local_only = "reads a node root"    # it needs the host, so it never enters a VM world
restart    = ["node1"]              # nodes restarted between driver and oracle (local worlds only)
facts      = ["object_id"]          # the facts its driver writes
timeout    = 180                    # the driver's and the oracle's budget, in seconds (default 120)
# ops      = ["enable-tor node1"]   # VM operations (netsim/ops/) run before the driver; imply
#                                   # destroys, and a test with ops is never local_only

[config.node1.objects]              # a module config override for one node, per world
external_registration_sweep_interval = "1s"
```

- **`nodes`** names nodes within the test's world: a name already used in
  that world is the same node. Tests that share a world add up their
  rosters, and root tests that destroy nothing share w0. A local world starts
  at most `[ports] span / 10` distinct nodes, 20 with `tests/config.toml`'s
  `span = 200`; a test that brings one more is FAIL/environment at
  `prepare`, and `selftest/test_plan.py` fails first for a shipped suite.
  `--plan` shows which tests share a world.
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
  that module, key by key; a module the harness builds nothing for gets the
  table alone. A local world builds a config for every module
  `lib/nodeconfig.py` names; a VM world builds apphost's and user's alone
  (`harness_configs`: the listener, the token, the E2E gate and the accepting
  policies, the same as a local node's), so there every other module gets
  the table alone. The config follows the world,
  not the test: the planner merges the overrides of every test in a world,
  a test that sets a key otherwise than its world forks into one of its
  own, and a clash with its own lineage is a plan error. `--plan` lists a
  world's overrides under its steps.
  - Keys the harness owns, like `apphost.tokens`, `apphost.bind_http` and
    the listeners' ports, are refused. `mcp.bind_mcp` may only be `""`,
    which turns MCP off: `messaging-ops` runs on
    `[config.nomcp1.mcp] bind_mcp = ""`.
  - A world writes each module's config as JSON, which astrald's YAML
    loader reads; a file it cannot parse, or a key the module does not
    have, stops the node. A VM world pushes its files into each guest with
    the binaries, before its ops, so a test whose override an op in its
    world would write over forks, and a lineage holding both is refused.
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

| suite | what it is | worlds (needs) | runs in |
|-------|------------|----------------|---------|
| `main.suite` | every test without `ops`, the everyday one | local (any host) | ~3 min; ~2 min on an M-series Mac |
| `tor.suite` | two nodes meet on a LAN, one leaves it, Tor holds | VM (netsim) | ~37 min |
| `nat.suite` | two nodes meet on a LAN, then hole-punch through NAT | VM (netsim) | not measured |
| `gateway.suite` | a NAT'd node reaches its swarm through a gateway | VM (netsim) | not measured |
| `agent.suite` | the flows an AI operator can drive (`--driver agent`) | VM (netsim) | ~1.5 h |

The plans of the shipped suites are pinned under `selftest/golden/`, so a
manifest change that moves a test shows up as a diff someone reads.

## Without netsim

A VM world needs netsim, and netsim runs only on Linux. Everything else
needs no VM: `main.suite`, any test without `ops`, `--isolate`, and
`--target attach`, on Linux or macOS.

A host that runs no VM world (not Linux, or no `netsim` on PATH) refuses a
run that needs one, whole and before anything is built, leased or written:
the tor, nat and gateway suites, any test with `ops`, `--vm`, `--save`,
`--from`, and `--driver agent`, whose operator lives in VM node1. The
refusal names each test and its ops, or the flag:

    $ ./tests/run tor-link
    run: this host runs no VM world (netsim is not on PATH), and this selection needs one:
      w0: tor-link runs VM ops (enable-tor node1 node2; leave-lan node2 --peer node1)
    run it on a Linux host with netsim, or select without these (tests/README.md, Without netsim)

`--plan` still prints the plan and exits 0, and notes on stderr which
worlds a run would refuse.

A first run on a new machine:

    git clone https://git.satforge.dev/astralp2p/astral-py   # path in config.toml
    ./tests/run --plan                 # what the default suite does
    ./tests/run                        # main.suite, local worlds
    cd tests && PYTHONPATH=.:<astral-py>/src python3 -m unittest discover -s selftest -t .

On Linux with netsim, `netsim doctor` checks the host and `./tests/link.sh`
registers this tree's ops once per checkout.

On macOS, the port lease misses a port another process holds on
`127.0.0.1` (`test_lease_ports_skips_a_bound_span` fails there), so a run
can collide with such a listener.

## Worlds

The planner (`lib/plan.py`) turns a selection into worlds, and the runner
runs them one after another, each from fresh machines:

- tests that only observe share a world, and run before anything builds;
- a test that builds state continues the world;
- every further builder, and every destroyer but one, gets a fork that
  replays its lineage as prereqs, by script;
- a destroyer with no `after` forks once a builder has run, since a fresh
  world is the only state it can name.

A world is one of three kinds:

| kind | the machines | when |
|------|--------------|------|
| local | astrald processes on loopback | the default |
| vm | the lab's VMs on a simulated LAN (`netsim/labs/`); needs netsim on a Linux host, refused elsewhere | a test with `ops`, `--vm`, or `--driver agent` |
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
  `{<node>.identity}`, `{<node>.alias}`, the roster name, and
  `{<node>.lan_endpoint}`, the address a peer links to. An unknown
  one is refused before the operator starts, as a `driver` failure, and so
  is any that would hand the operator a token: it must never hold what it
  is judged with;
- a prompt holds in either world. It names a node by `{<node>.alias}`
  together with `{<node>.identity}`, since a roster name resolves on a node
  only where a script set it as an alias and a node's own alias is random.
  It gives `{<node>.lan_endpoint}` when the flow links to a peer, and it
  never says hostname, local network or this node: `selftest/test_prompts.py`
  holds every prompt to that;
- a footer asks for exactly the test's `facts`, in `~/.facts/<test>.json`.
  That file alone is collected, and a declared fact missing from it is a
  `driver` failure. It is removed before the session, so no earlier copy
  answers for the test; the rest of `~/.facts/` stays, so the User token
  bootstrap's operator keeps there outlives its session;
- the model is `config.toml`'s `[agent] model`, set in the operator once
  per world; `base_url` applies only with a model. The report names the
  model, since a red under one model says nothing about another;
- an operator session gets `agent.TIMEOUT`, 3600 s: one measured session
  took 2404 s. Past it, every `tester` process on `node1` is killed, and
  the test is a `driver` failure.

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

Every target strips the ambient endpoint and token variables from the
driver environment: a developer's `ASTRALD_APPHOST_TOKEN` would otherwise
answer for a test's anonymous connect. `--target attach` reads them in the
runner to find your daemon, and drivers reach it through `session.json`. The
daemon's apphost gate must admit the test's ops: a `fresh` node under the
default gate answers only `apphost.register_manager` and `user.info`, so attach
to a claimed node, or give a fresh one the harness's gate
(`lib/nodeconfig.E2E_GATE`).

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

| exit | means |
|------|-------|
| 0 | PASS |
| 1 | FAIL or INCOMPLETE; a preflight refusal (astral-py missing or older than `astral_py.requires`, Python < 3.11); or a harness crash, which prints a traceback and leaves no results directory |
| 2 | refused before anything ran, with no results directory: a plan error, a host that runs no VM world, busy ports, a usage error |
| 130 | interrupted by Ctrl-C |

Under `results/<stamp>/`:

- `report.md` for people, `results.json` and `events.jsonl` for machines
  (schema in `lib/results.py`); the header names the host, its OS and
  release (`host_os`) and its machine (`host_arch`);
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

Every run, on Linux or macOS: Python ≥ 3.11, a Go toolchain, and an
astral-py checkout (path in `config.toml`) — imported directly from its
`src/`, since the package has zero dependencies. The checkout holds the
commit `config.toml` `astral_py.requires` names; `tests/run` refuses an older
one before it builds anything. No venv, no pip.

VM worlds: a Linux host with a working `netsim` on PATH (`netsim doctor`),
and `./tests/link.sh` run once per checkout to register this tree's netsim
ops.

The harness's own selftests take a second and touch no daemon:

    cd tests && PYTHONPATH=.:<astral-py>/src python3 -m unittest discover -s selftest -t .
