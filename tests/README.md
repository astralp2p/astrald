# tests/ — the Astral integration testing system

One command, real daemons, deterministic verdicts:

    ./tests/run                     # the default suite (suites/main.suite)
    ./tests/run main.suite          # a named suite, in its listed order
    ./tests/run adopt-node          # one test + the fixture prefix it needs
    ./tests/run smoke adopt-node    # several tests
    ./tests/run adopt-node --keep   # leave the world running afterwards

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

## Requirements

Python ≥ 3.11, a Go toolchain, and an astral-py checkout (path in
`config.toml`) — imported directly from its `src/`, since the package has
zero dependencies. No venv, no pip.

## Milestone state

M2 ships the shape: env `node`, `--target fresh`, `--driver script`. The
rest of the surface parses and reports where it arrives — `--driver agent`
and `--target attach` in M5, `--target stage:<name>` and env `netsim` in M4.

`net/` and `stages/` are pre-unification residue, absorbed-pending: the
netsim stories and flow tasks still run under `netsim story`, and M4
retires them as `e2e/` tests. Register them and the vmops with
`./tests/net/link.sh`.

Manifests are TOML (stdlib), which the design document also writes.

## Known issue

`bootstrap-user-software-key` intermittently fails with
`auth.sign_contract: sign as issuer: unsupported` — a pre-existing astrald
race: the crypto module indexes a stored private key asynchronously, and a
`sign_contract` arriving before the index lands finds no signer. Observed
on roughly half of the bootstraps on this host. Re-run until the
daemon-side fix lands.
