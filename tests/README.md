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

A test reaches its nodes through three things only, so it runs in any world
that fits it:

- the **session view**: `endpoint` and `token` to query a node, `identity`,
  `lan_endpoint` for a peer to link to, and the rest of `NODE_FIELDS`;
- **`host_ip`**: the address on the test host that this node dials to reach
  a listener the test serves there (a helper app, an authority, a bait).
  The test binds the listener at it and declares `host_listener`. A local
  world gives `127.0.0.1`; an attach world gives `127.0.0.1` when its
  endpoint is a unix socket or a loopback TCP address, else `null`; a Docker
  world gives its network's gateway; a VM world gives `null`;
- **`lib/nodefs.py`**: a node's files and its `astrald.log`, through the
  node's `root` (see [Node files](#node-files)). A test never builds a host
  path from a `root`.

A test that needs anything else is `local_only`, with the reason.

**2. Write the manifest.** Every key but `nodes` is optional:

```toml
nodes      = ["node1", "node2"]     # the roster: names this test's nodes
after      = "adopt-node"           # the test whose world this one continues; absent: a root test
destroys   = true                   # only this test's descendants may follow it in its world
local_only = "reads a host path"    # it needs the host, so it runs only in a local world
host_listener = "serves an app node1 dials"  # it serves a listener on the test host at host_ip;
#                                   # never in a VM world
restart    = ["node1"]              # nodes restarted between driver and oracle (local or Docker worlds);
#                                   # an app that must survive it: Long-lived apps
stop       = ["node1"]              # nodes stopped for good between driver and oracle (local or Docker worlds); implies destroys
facts      = ["object_id"]          # the facts its driver writes
timeout    = 180                    # the driver's and the oracle's budget, in seconds (default 120)
# ops      = ["enable-tor node1"]   # VM operations (netsim/ops/) run before the driver; imply
#                                   # destroys, and a test with ops is never local_only
#                                   # nor host_listener

[config.node1.objects]              # a module config override for one node, per world
external_registration_sweep_interval = "1s"
```

- **`nodes`** names nodes within the test's world: a name already used in
  that world is the same node. Tests that share a world add up their
  rosters, and root tests that destroy nothing share w0. A local world starts
  at most `[ports] span / 10` distinct nodes, 30 with `tests/config.toml`'s
  `span = 300`; a test that brings one more is FAIL/environment at
  `prepare`, and `selftest/test_plan.py` fails first for a shipped suite.
  `--plan` shows which tests share a world.
- **`after`** is the whole ordering model: a test names the test whose
  world it continues, never a state. A new test needs only its files and an
  `after`.
- **`destroys`** marks a test that leaves its world unusable for anything
  but its own descendants: it expels a node, purges a repository, moves a
  node behind NAT, or claims a node another test needs unclaimed.
- **`local_only`** is only for a reason no world operation serves: the
  session view, `host_ip` and `lib/nodefs.py` cover querying a node,
  serving it a listener and reaching its files and log. A local_only test
  runs in local worlds only, so a Docker run refuses it. A roster is not a
  reason: a test whose `nodes` name a machine the VM lab does not run
  (`plan.LAB_NODES`: node1, node2, reflector) never enters a VM world, and
  runs in a local or Docker world as any other test does. A restart is not
  a reason either.
- **`host_listener`** says why a node dials a listener the test serves on
  the test host, bound at the node's `host_ip`. The planner refuses such a
  test only in a VM world, whose nodes have no `host_ip`. Under `--target
  attach` it plans, and a daemon off this host (`host_ip` null) is
  FAIL/environment at `prepare`. A test may be both `host_listener` and
  `local_only` for another reason.
- **`restart`** restarts the named nodes after the driver, in a local or
  Docker world, on the state they hold. The planner never puts a test with
  `restart` in a VM world or on an attached daemon.
- **`stop`** stops the named nodes after the driver, in a local or Docker
  world, and leaves them stopped. A stopped node is not dead: the liveness
  check skips it, and the session view of the test's oracle and of every
  later test in the world omits it. A later test whose roster names a
  stopped node, or a restart of one, is FAIL/environment at `prepare`. A
  test with `stop` never enters a VM world, like a test whose roster
  leaves the VM lab.
  `introduce-node-setup` stops the node that generated the User's words.
- **`facts`** is a contract. An oracle may read only facts declared on its
  `after` chain (`selftest/test_shipped.py` checks), and the agent driver
  asks the operator for exactly these.
- **`[config.<node>.<module>]`** writes over the harness's own config for
  that module, key by key; a module the harness builds nothing for gets the
  table alone. A local world builds a config for every module
  `lib/nodeconfig.py` names; a VM world builds apphost's and user's alone
  (`harness_configs`: the listener, the token, the E2E gate and the accepting
  policies, the same as a local node's), and a Docker world those two, mcp's
  (the server and the declared tools) and auth's (the authority at
  `authority_url`), so there every other module gets the table alone. The config follows the world,
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
| `nearby.suite` | an unowned node adopted by its nearby alias, two nodes of one User meeting in stealth, a Silent node, a sibling relinked when heard | VM (netsim) | ~75 min estimated (not measured) |
| `agent.suite` | the flows an AI operator can drive (`--driver agent`) | VM (netsim), or Docker without netsim | not measured |
| `integration.suite` | full-integration flows in throwaway containers (`--docker`) | Docker, or local without `--docker` | not measured |

The plans of the shipped suites are pinned under `selftest/golden/`, so a
manifest change that moves a test shows up as a diff someone reads.

## Without netsim

A VM world needs netsim, and netsim runs only on Linux. Everything else
needs no VM: `main.suite`, any test without `ops`, `--isolate`, and
`--target attach`, on Linux or macOS. Full integration runs — the agent
driver, or `--docker` — run in Docker worlds instead (see World kinds).

A host that runs no VM world (not Linux, or no `netsim` on PATH) refuses a
run that needs one, whole and before anything is built, leased or written:
the tor, nat, gateway and nearby suites, any test with `ops`, `--vm`,
`--save` and `--from`. The refusal names each test and its ops, or the flag:

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

On macOS, see [Requirements, macOS](#macos) first.

## Worlds

The planner (`lib/plan.py`) turns a selection into worlds, and the runner
runs them one after another, each from fresh machines:

- tests that only observe share a world, and run before anything builds;
- a test that builds state continues the world;
- every further builder, and every destroyer but one, gets a fork that
  replays its lineage as prereqs, by script;
- a destroyer with no `after` forks once a builder has run, since a fresh
  world is the only state it can name.

## World kinds

A world is one of four kinds:

| kind | the machines | when | needs |
|------|--------------|------|-------|
| local | astrald processes on loopback | the default: simple e2e | nothing |
| attach | the daemon you already run | `--target attach` | nothing |
| docker | one throwaway container per node, on one Docker network | `--docker`, or `--driver agent` on a host without netsim | Docker Desktop (macOS) or Docker Engine (Linux) |
| vm | the lab's VMs on a simulated LAN (`netsim/labs/`) | a test with `ops`, `--vm`, or `--driver agent` where netsim runs | netsim on a Linux host; refused elsewhere |

A Docker world is the lab's shape without netsim. Each node runs in its own
container from `docker/Dockerfile`, the containers share a network, and
node1's container holds the lab's operator (`tester`, Qwen Code), so
`--driver agent` drives it as it drives VM node1. A node's apphost, and
its MCP server unless the node's `mcp.bind_mcp` is `""`, are published on
the host's `127.0.0.1` at its leased ports, which is what drivers and
oracles dial (`endpoint`, `mcp_url`). No host file or environment variable enters a
container. The run copies in an astrald and astral-query built for Linux
(`.cache/linux-<arch>/`). The operator's astral-agent skill comes with the
image: the Astral docs at the commit this checkout pins in `.ai/system`,
fetched from github.com/astralp2p/astral-docs when the image builds, and
`docker/astral-agent.md` as its `SKILL.md`. Nothing need be installed on the
host. `[agent] skill` in `config.toml` names a skill whose `SKILL.md` and
`references/` replace the image's, e.g. while working on the skill; the
docs stay the image's.

The first Docker run builds the node image, which takes minutes and needs
npm's registry and GitHub; later runs reuse it until `docker/` or the
`.ai/system` pin changes. A test with
`ops` needs a VM world, and a `local_only` test a local one: a Docker run
refuses both, as `--save` and `--from`. A Docker world restarts and stops a
node's astrald in its container. A node reaches a listener the test serves
on the host at its `host_ip`, the gateway of the world's network
(`docker network inspect`, or the container's default route when the
network names none), and a test reaches a node's files with
`lib/nodefs.py`, through `docker exec`. At a world's end its containers and
network are removed; `--keep` leaves them up and names them, and later
runs leave a kept world alone until you remove it (`docker rm -f`). A run
killed by SIGKILL leaves its containers, and the next Docker run removes
them.

The live Docker selftests build the image and start two containers:

    cd tests && ASTRAL_TESTS_DOCKER=1 PYTHONPATH=.:<astral-py>/src python3 -m unittest selftest.test_docker_live -v

`./tests/check-docker.sh [--agent]` runs the whole no-netsim check in order —
selftests, the live Docker selftests, `main.suite`, `integration.suite
--docker`, nothing left behind, and with `--agent` the agent suite — and
gathers each step's log, verdict and failing entries' logs into
`results/check-docker-<stamp>/`.

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
Qwen Code on `node1` with the astral-agent skill — baked into the lab's VM,
or in node1's container in a Docker world — and `lib/agent.py` drives it the
same way in both:

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

## Shared Go helpers

A directory under `e2e/` without a `test.toml` is not a test, and the planner
never loads it. `e2e/internal/provider` is the Go helper of the
object-provider drivers: the session loader, app registration with
ServeObjects permits, and apps that serve `objects.find`, `describe`,
`search` and `read` with counting handlers in answer, claim or hang mode. Its
`README.md` lists the calls.

## Long-lived apps

A driver's own processes end with it, and a manifest's `restart` restarts
nodes between the driver and the oracle. A test about an app that must
outlive a node restart starts it with `lib/apps.py` instead; the first user is
`app-node-restart-reconnect`.

```python
from lib import apps

binary = apps.build_go("tests/e2e/my-test/driver")      # go build into the world's bin/
app = apps.spawn("my-test-provider", [binary, "serve"],  # a name unique in the world
                 env={"APP_NODE_ENDPOINT": node["endpoint"],
                      "APP_NODE_TOKEN": node["token"]})  # secrets go in env, not argv
state = app.wait_state(lambda s: s.get("attaches", 0) >= 1, timeout=30)
# ... the driver ends; the world restarts a node; in the oracle:
app = apps.get("my-test-provider")
assert app.alive() and app.state()["pid"] == app.pid
app.stop()
```

| call | does |
|------|------|
| `build_go(package, name=None)` | builds a Go package of this repository, a path as `go build` takes it, into the world's `bin/`; returns the binary |
| `spawn(name, argv, env=None, cwd=None)` | starts `argv` detached, in a session of its own, and returns an `App`; `AppError` when `name` still runs |
| `get(name)` | the `App` an earlier script of the world spawned, running or not |
| `App.alive()` / `.pid` / `.log` | whether the same process runs (pid and kernel start time, so a reused pid is not it) / its pid / its stdout and stderr file |
| `App.state()` / `.wait_state(ready, timeout)` | the JSON the app last wrote to `$ASTRAL_TESTS_APP_STATE` (`{}` before it wrote one) / waits for `ready(state)`; `AppError` with the log's tail when the app dies first or time runs out |
| `App.stop(grace=8)` | SIGTERM to the app's process group, SIGKILL after `grace` s; stopping a stopped app is no error |
| `stop_all()` / `reap(dir)` | stops every app of the world / of directory `dir` (what the runner calls) |

The app finds its state file in `ASTRAL_TESTS_APP_STATE` and rewrites the whole
file, by rename, whenever it has news: readiness, what it served, errors it
saw. That file is how the oracle observes the app's side without a protocol.

The runner sets `ASTRAL_TESTS_APPS` for every driver and oracle step to
`results/<stamp>/<world>/apps/`, one directory per world: `<name>.app.json`
(pid, start time, argv), `<name>.state.json`, `<name>.log` and `bin/`. Apps
are therefore named world-wide, so a test prefixes its own.

The runner stops every app of a world, process groups and all, before it
closes the world's nodes: after a pass, after a failed driver or oracle, on
Ctrl-C and on SIGTERM or SIGHUP, which `tests/run` unwinds like Ctrl-C. A test
that ends its app itself (`stop()` in the oracle) leaves the world as the next
test expects it. Limits: a SIGKILLed runner cannot reap, so the apps stay up
with their entries in the results directory (`pkill -f results/<stamp>/`); a
`--keep` world keeps its apps running with its nodes; an app is a process on the
host, so a node that dials it reaches it at the node's `host_ip`, and its test
declares `host_listener`.

## Node files

`lib/nodefs.py` reads and writes a node's files through its session-view
`root`: a host path in a local world, `docker:<container>:<path>` in a Docker
world (through `docker exec`). Every call takes the node's entry and a path
relative to its root; any other root (a VM world's, an attached daemon's
`null`) raises `NodeFSError`. A Docker command that fails raises `OSError`.

| call | does |
|------|------|
| `path(node, rel="")` | the absolute path as the node's machine sees it, e.g. for `fs.new_watch`; resolved on a host |
| `read_bytes(node, rel)` / `read_text(node, rel)` | the file's bytes / its text, a non-UTF-8 byte as U+FFFD |
| `write_bytes(node, rel, data)` | writes the file, creating its parent directories |
| `mkdir(node, rel)` | creates the directory and its parents; one already there stays |
| `remove(node, rel)` | removes a file, a symlink (never followed) or a directory tree |
| `symlink(node, rel, target)` | makes `rel` a symlink to `target`, also relative to the root |
| `rename(node, src, dst)` | moves `src` to `dst`, replacing a file there |
| `listdir(node, rel="")` | `{name: kind}`, kind `file`, `dir`, `link` or `other` |
| `set_mtime(node, rel, mtime)` | sets the modification (and access) time, in seconds since the epoch |
| `log(node)` | the node's `astrald.log` as text |

## Host directories

`lib/fsfixture.py` holds what the fs tests share. astral-py binds no `fs`
module, so the ops are raw queries.

| call | does |
|------|------|
| `new_watch(c, path, name, temporary=False)` / `new_repo(...)` | `fs.new_watch` / `fs.new_repo`; None on the ack, else the node's error text |
| `disk_ids(root)` | `{path: id}` of every regular file under `root`, hashed here; symlinks skipped |
| `wait_scan(c, repo, want, deadline)` | polls `objects.scan` until it lists exactly `want`; `(ok, last listing)` |
| `wait_contains(c, repo, ids, deadline)` | polls `objects.contains`; the ids still missing |
| `file_locations(c, oid)` | `[(Path, NodeID)]` of the `mod.fs.file_location` descriptors, after `objects.learn` |
| `read_refused(c, oid, repo)` | None when a read is refused, else what was served |

`lib/indexing.py` drives the indexing ops as raw JSON queries, since
astral-py binds no `indexing` module: `register_indexer`, `answer` for a
one-shot op, `Subscription` for `indexing.subscribe` (one change at a time,
acked with `ack`), `drain`, and `change`, which reduces an envelope to its
type, repository, version and object.

## Fast loops

- **`--keep`** leaves the run's last world up: a local world's nodes, a
  Docker world's containers, or a VM world's simulation with its tunnels.
- **`--target attach`** runs a selection against a daemon you already run,
  resolved the way astral-py resolves one (`ASTRAL_ENDPOINT`,
  `ASTRALD_APPHOST_TOKEN` and friends). It is the code-and-debug loop, with
  open eyes: nothing is torn down, and a `destroys` test really changes your
  daemon and says so first. The plan is refused when the selection needs a
  prereq (attach checks a state, it never builds one), a roster names more
  than one node, a test is `local_only` or restarts a node, or the driver is
  `agent`. A `host_listener` test runs when the daemon is on this host.
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
default gate answers only `sys.register_manager` and `user.info`, so attach
to a claimed node, or give a fresh one the harness's gate
(`lib/nodeconfig.E2E_GATE`).

## Reading a failure

`report.md` names each failure's kind and the phase it stopped at:

| failure kind | means |
|--------------|-------|
| `driver` | the flow never happened — the driver could not perform it |
| `verify` | astrald misbehaved — the flow ran and the oracle rejected it |
| `oracle` | the oracle broke on its own code (a TypeError, KeyError, NameError…) — no verdict about astrald |
| `environment` | the world broke — no verdict about astrald is possible |

| phase | the step that stopped |
|-------|-----------------------|
| `prepare` | bringing the roster up and running its `ops` |
| `driver` | `script.py`, or the operator |
| `restart` | the restart `restart` names |
| `stop` | the stop `stop` names (v2.6) |
| `verify` | `verify.py` |
| `alive` | a node died under the test |

A skipped test carries a `reason`: the test on its `after` chain that failed
in its world, the world dying, or the run being interrupted. A run holding a
skip reports INCOMPLETE, never PASS; a failure outranks a skip.

| exit | means |
|------|-------|
| 0 | PASS |
| 1 | FAIL or INCOMPLETE; a preflight refusal (astral-py missing or older than `astral_py.requires`, Python < 3.11); or a harness crash, which prints a traceback and leaves no results directory |
| 2 | refused before anything ran, with no results directory: a plan error, a host that runs no VM world, busy ports, a TMPDIR too long for an app's unix socket, a usage error |
| 130 | interrupted by Ctrl-C |
| 143 | ended by SIGTERM, with the report written |
| 129 | ended by SIGHUP, with the report written; under `nohup` SIGHUP stays ignored |

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
  `w<N>/logs/`: a VM world's guest journals, or a Docker world's astrald
  logs.

The report is written however the run ends — an exception, Ctrl-C, SIGTERM
or SIGHUP, a world that failed to close (SIGKILL writes none) — and `./tests/summary` lists what has passed here
across runs.

## Requirements

Every run, on Linux or macOS: Python ≥ 3.11, a Go toolchain, and an
astral-py checkout (path in `config.toml`) — imported directly from its
`src/`, since the package has zero dependencies. The checkout holds the
commit `config.toml` `astral_py.requires` names; `tests/run` refuses an older
one before it builds anything. No venv, no pip.

Docker worlds: a `docker` CLI whose engine answers (Docker Desktop on
macOS, Docker Engine on Linux). A host without one refuses a run that needs
a Docker world, before anything is built.

VM worlds: a Linux host with a working `netsim` on PATH (`netsim doctor`),
and `./tests/link.sh` run once per checkout to register this tree's netsim
ops.

### macOS

- Use Homebrew's `python3` (3.11 or later). macOS's own `/usr/bin/python3`
  is 3.9, and `tests/run` refuses it.
- Keep TMPDIR at 72 characters or fewer, or run with `TMPDIR=/tmp`: the
  drivers that serve an app bind a unix socket under it, and macOS caps
  that path at 103 characters. A run with a local or attached world tries
  that bind first, and refuses with exit 2 when it fails. Runs that may overlap on one host use the
  same TMPDIR, because the port-lease locks live there.
- Off Linux a node gets no parent-death signal, so after a SIGKILL the
  nodes keep running. Stop them from the repo root with
  `pkill -INT -f "$(pwd -P)/tests/.cache/astrald -root"`, which also stops
  a `--keep` world; the `-P` matters because a node's argv carries the
  resolved path. Their roots stay under `$TMPDIR/astrald-tests-*`.
- Node logs hold `[nearby] push error … sendto: message too long` lines:
  macOS caps a UDP datagram below nearby's status push. They do not affect
  verdicts.
- The application firewall may ask whether the rebuilt
  `tests/.cache/astrald` may accept incoming connections. Loopback works
  either way.

The harness's own selftests take a second and touch no daemon:

    cd tests && PYTHONPATH=.:<astral-py>/src python3 -m unittest discover -s selftest -t .
