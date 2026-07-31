# tests/ — the Astral integration testing system

One command, real daemons, deterministic verdicts:

    ./tests/run                     # the default suite (suites/main.suite)
    ./tests/run main.suite          # a named suite, in its listed order
    ./tests/run adopt-node          # one test + the fixture prefix it needs
    ./tests/run smoke adopt-node    # several tests
    ./tests/run adopt-node --keep   # leave the world running afterwards
    ./tests/run smoke --target attach   # judge a daemon that is already up

Design: https://wiki.satforge.dev/doc/integration-testing-system-tAKjyvbcHo

## Three nouns

- A **test** is a directory under `e2e/`: `test.toml` (the manifest),
  `script.py` (the driver), `verify.py` (the oracle — the only judge),
  `README.md`. Every e2e test lives here, both envs; `env` is a manifest
  field, not a directory.
- A **suite** is a file under `suites/`: these tests, in this order. A suite
  composes; it never restates what a test needs.
- A **fixture** builds world state: `fixtures/vmops/` holds the named VM
  operations a netsim test runs as steps, `fixtures/lab/` the netsim base
  recipe.

## The manifest

```toml
env     = "node"            # the cheapest world that can falsify the test
start   = "one-node"        # the state the test begins from
saves   = "two-nodes"       # the state it leaves behind, if any
mutates = false             # true when it invalidates its start state
nodes   = ["node1", "node2"]
steps   = []                # env netsim only: vm:<op> … and driver
drivers = ["script"]        # script, agent, or both
timeout = 180
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
- A bare test selection gets the fixture prefix its start needs, derived
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

`main.suite` is the env-node chain, seven tests in about twelve seconds.
`import-user-software-key` stays out of it: `start = "null"` means a
pristine node, and env `node` runs one live session, so the chain cannot go
back to an unclaimed node1 mid-suite.

## Where the machines come from

`--target` is orthogonal to `env` and to `--driver`:

- `fresh` (default) — spawn per run. Hermetic.
- `stage:<name>` — boot one simulation and run the whole selection against
  it. Needs the netsim executor.
- `attach` — no spawning: run against a daemon that is already up, resolved
  the way astral-py resolves one (`ASTRAL_ENDPOINT`, `ASTRALD_APPHOST_TOKEN`
  and friends). The code-and-debug loop: point a test at the daemon you are
  hacking on and let the standard oracle judge it.

Under `attach` the run is not hermetic, so the header carries
`hermetic: false` and the ambient endpoint and token are deliberately left in
the driver environment. Two guards make it safe to use on a daemon you care
about: a start state is **checked, never built** — a selection that would
need a fixture prefix is refused rather than run at your daemon — and a
`mutates` test prints a warning naming itself before anything happens.
Attach has one daemon, so a test with a multi-node roster is refused too.

## Scripted and AI-driven

A test declares its drivers, and the same oracle judges every one of them.
That is what makes the split useful: **script red means astrald broke; agent
red while script is green means the operator or its skill broke.** A driver
failure and an oracle failure are already distinct in the results
(`failure_kind` `driver` versus `verify`), so a run says which of the three
went wrong without anyone reading a log.

## Requirements

Python ≥ 3.11, a Go toolchain, and an astral-py checkout (path in
`config.toml`) — imported directly from its `src/`, since the package has
zero dependencies. No venv, no pip.

## Milestone state

Working today: env `node`, `--driver script`, `--target fresh` and
`--target attach`.

Not working, and the runner says so rather than guessing: env `netsim`,
`--target stage:<name>` and `--driver agent`. All three need VMs, and netsim
does not run on this host — `NETSIM_STAGES_DIR` points at a root-owned
`/mnt/netsim`, so netsim cannot create its staging directory. The netsim
executor exists (`lib/executors/netsim.py`) but its `session.json` carries no
working endpoints: a NAT'd node's apphost is netns-local, and the host-side
tunnel that reaches it cannot be designed without a live netsim. Seven tests
already ship the `prompt.md` the agent driver will use.

`net/` and `stages/` are pre-unification residue, absorbed-pending: the
netsim stories and flow tasks still run under `netsim story`, and M4
retires them as `e2e/` tests. Register them and the vmops with
`./tests/net/link.sh`.

Manifests are TOML (stdlib), which the design document also writes.

## Known issue

Two pre-existing astrald startup races flake the chain root. Neither is
introduced here — this tree changes no daemon code — and the runner does not
retry, because a retry would hide them. Re-run until the daemon-side fixes
land.

- `auth.sign_contract: sign as issuer: unsupported` — the crypto module
  indexes a stored private key asynchronously, and a `sign_contract`
  arriving before the index lands finds no signer.
- `panic: database is locked (5) (SQLITE_BUSY)` — modules load concurrently
  and contend on the node's own sqlite file. The DSN
  (`core/assets/core_assets.go:117-122`) sets no `busy_timeout`, so there is
  no configuration the harness could supply to absorb it.
