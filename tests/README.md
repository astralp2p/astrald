# tests/ — the Astral integration testing system

One command, real daemons, deterministic verdicts:

    ./tests/run                     # the default suite (suites/main.suite)
    ./tests/run main.suite          # a named suite, in its listed order
    ./tests/run adopt-node          # one test + the prereq prefix it needs
    ./tests/run smoke adopt-node    # several tests
    ./tests/run adopt-node --keep   # leave the world running afterwards
    ./tests/run smoke --target attach   # judge a daemon that is already up

Design: https://wiki.satforge.dev/doc/integration-testing-system-tAKjyvbcHo

## Start here: suite, env, driver

Three independent choices. Pick each one separately — they do not constrain
each other.

**A suite is *what* runs.** A file under `suites/`, listing tests in the order
they should run. Nothing more.

    ./tests/run main.suite      # the whole env-node chain, seconds
    ./tests/run adopt-node      # no suite: one test, and whatever it needs first

| suite | what it is | env | runs in |
|-------|------------|-----|---------|
| `main.suite` | the whole node chain, the everyday one | node | seconds |
| `tor.suite` | two nodes meet on a LAN, one leaves it, Tor holds | netsim | ~37 min |
| `substrate.suite` | both transport tests: Tor, then NAT hole-punching | netsim | ~1 h |
| `agent.suite` | the flows an AI operator can drive, one world, in order | netsim | ~1.5 h |

**An env is *where* it runs** — the world the test needs. A manifest field.

| env | the world | costs |
|-----|-----------|-------|
| `node` | astrald processes on loopback | seconds |
| `netsim` | real VMs on a simulated LAN | minutes |

`env` is a floor, not a ceiling. A `node` test runs happily in VMs when
something else in the run needs them; it never notices, because it reads
endpoints from `session.json` and cannot tell a process from a VM.

**A driver is *who* performs the flow.**

| driver | who | how |
|--------|-----|-----|
| `script` | `script.py` | code, exact and fast |
| `agent` | an AI operator in the lab | reads `prompt.md` and works it out |

    ./tests/run main.suite --driver agent

Both are judged by the same `verify.py`, and that is the whole point: **script
red means astrald broke; agent red while script is green means the operator or
its prompt broke.**

There is a fourth knob, `--target`, for where the machines come from — see
[Where the machines come from](#where-the-machines-come-from). The default
spawns a fresh world and is what you want unless you know otherwise.

## The nouns

- A **test** is a directory under `e2e/`: `test.toml` (the manifest),
  `script.py` (the driver), `verify.py` (the oracle — the only judge),
  `README.md`. Every e2e test lives here, both envs; `env` is a manifest
  field, not a directory.
- A **suite** is a file under `suites/`: these tests, in this order. A suite
  composes; it never restates what a test needs.
- A **lab** is a world recipe: `netsim/labs/two-node/` builds the VMs, the
  daemons and the operator that netsim tests start from.
- An **op** is a named machine operation: `netsim/ops/` holds every one, both
  the ops a lab recipe builds with and the ops a test runs as steps. netsim
  keeps one flat task namespace and cannot tell those apart, so neither does
  the tree.

## The manifest

```toml
env     = "node"            # the cheapest world that can falsify the test
start   = "one-node"        # the state the test begins from
saves   = "two-nodes"       # the state it leaves behind, if any
mutates = false             # true when it invalidates its start state
nodes   = ["node1", "node2"]
steps   = []                # env netsim only: vm:<op> … and driver
drivers = ["script"]        # script, agent, or both
timeout = 180               # the scripted flow's budget
agent_timeout = 2400         # an operator plans and works the node through its own shell
```

`env` is a minimum, not a prison: drivers and oracles read `session.json`
and never learn whether the endpoints behind it are processes on loopback
or VMs behind a tunnel.

## States

Tests chain through named states — `null → one-node → two-nodes → …` — one
namespace across both envs. A state has at most one producer.

- A suite is valid when a linear walk of its listed order satisfies every
  test's `start`: the start is the walk state or one of its ancestors.
- A test with `mutates = true` narrows the walk to its own branch; nothing
  downstream may consume the state it invalidated.
- A bare test selection gets the prereq prefix its start needs, derived
  from the chain.
- A failure skips every test whose start stands on the state that broke.

## Output

Every run writes `results/<stamp>/results.json` and `events.jsonl` (schema
in `lib/results.py`): a header — `astrald_ref`, `astral_py_ref`, `host`,
`sandbox`, `hermetic` — and one record per test × driver carrying `env`,
`status` ∈ pass/fail/skipped and, on failure, `failure_kind` ∈ `driver`
(the flow never happened) / `verify` (astrald misbehaved) / `environment`
(the world broke). Per-test driver and oracle logs sit beside them; each
node's astrald log lands under `results/<stamp>/session/`.

A hermetic run strips astral-py's ambient endpoint and token variables from
the driver environment: an `ASTRALD_APPHOST_TOKEN` in the shell otherwise
answers for a test's anonymous connect and the test node refuses it.

## Coverage

Every story of the catalog, in the cheapest env that can falsify it:

| Story | Test | Env | States |
|-------|------|-----|--------|
| 0001 | `bootstrap-user-software-key` | node | `null` → `one-node` |
| 0002 | `import-user-software-key` | node | `null` → — (runs alone) |
| 0003 | `adopt-node` | node | `one-node` → `two-nodes` |
| 0004 | `tor-link` | netsim | `two-nodes` → `two-nodes-tor` |
| 0005 | `nat-punch` | netsim | `two-nodes` → `two-nodes-nat` |
| 0006 | `object-store` | node | `two-nodes` → `two-nodes-data` |
| 0007 | `object-store-peer` | node | `two-nodes` → `two-nodes-data-peer` |
| 0008 | `read-remote-peer` | node | `two-nodes-data-peer` → `two-nodes-data-read` |
| 0009 | `expel-node` | node | `two-nodes` → `two-nodes-expel` |
| — | `smoke` | node | `null` → — |
| — | `app-query` | node | `two-nodes` → — |
| — | `app-contract` | node | `two-nodes` → — |

`main.suite` is the env-node chain, and it runs in seconds.
`import-user-software-key` stays out of it: `start = "null"` means a
pristine node, and env `node` runs one live session, so the chain cannot go
back to an unclaimed node1 mid-suite.

`app-contract` stays out for a different reason: one of its six acts is red on
master by design, holding an open question about whether a contract is
required for an app to be reachable on its own node, and a suite that is always
red stops reporting regressions. It joins `main.suite` when it goes green.

## Where the machines come from

`--target` is orthogonal to `env` and to `--driver`:

- `fresh` (default) — spawn per run. Hermetic.
- `stage:<name>` — boot that stage once and run the whole selection against
  it: no chain walk, no per-test state, and nothing saved back. Naming the
  stage is the assertion that the world already stands where the tests need
  it, the same bargain attach strikes for a running daemon.
- `attach` — no spawning: run against a daemon that is already up, resolved
  the way astral-py resolves one (`ASTRAL_ENDPOINT`, `ASTRALD_APPHOST_TOKEN`
  and friends). The code-and-debug loop: point a test at the daemon you are
  hacking on and let the standard oracle judge it.

Under `attach` the run is not hermetic, so the header carries
`hermetic: false` and the ambient endpoint and token are deliberately left in
the driver environment. Two guards make it safe to use on a daemon you care
about: a start state is **checked, never built** — a selection that would
need a prereq prefix is refused rather than run at your daemon — and a
`mutates` test prints a warning naming itself before anything happens.
Attach has one daemon, so a test with a multi-node roster is refused too.

## Scripted and AI-driven

A test declares its drivers, and the same oracle judges every one of them.
That is what makes the split useful: **script red means astrald broke; agent
red while script is green means the operator or its skill broke.** A driver
failure and an oracle failure are already distinct in the results
(`failure_kind` `driver` versus `verify`), so a run says which of the three
went wrong without anyone reading a log.

A test that declares the `agent` driver ships a `prompt.md`: the flow in
plain words, the way a person would ask for it. `smoke` and `nat-punch`
declare `script` only — neither is a flow anyone would ask an operator to
perform.

The operator is the Qwen Code agent the lab bakes into `node1`, equipped
with the `astral-agent` skill. It reaches astrald over the guest's own
apphost, the way an app does, so `--driver agent` runs in VMs whatever the
selected tests declare: a manifest's `env` is the cheapest world that can
falsify the test, never a ceiling. Which model it drives with is a property
of the run — `config.toml [agent] model`, rewritten into the operator after
boot — so comparing two models costs a re-run, not a twenty-minute rebake.
The report names the model, because a red under one model says nothing
about another.

## The report

Every run writes `results/<stamp>/report.md`: the verdict, what was run,
every test with its time, and — when something is red — which of the three
layers broke, what that means in words, and where its log is. The runner
prints the one-line verdict and the path. That file is the thing to hand
someone; `results.json` is for machines.

A run holding a skipped test reports INCOMPLETE, never PASS. A skip carries
no verdict, and a green-looking document over an unbuilt state would be
lying about exactly the case the third status exists for.

## Requirements

Python ≥ 3.11, a Go toolchain, and an astral-py checkout (path in
`config.toml`) — imported directly from its `src/`, since the package has
zero dependencies. No venv, no pip. Env `netsim` additionally needs a
working `netsim` on PATH. `./tests/link.sh` registers this tree's
netsim ops — the ones a test dispatches as steps and the ones a lab recipe
builds the world with, which live together because netsim cannot tell them
apart; run it once per checkout.

Manifests are TOML (stdlib), which the design document also writes.
