# tests/ — the Astral integration testing system (M1: walking skeleton)

One command, real daemons, deterministic verdicts:

    ./tests/run --lane node                       # whole node lane
    ./tests/run --lane node --only adopt-node     # one test + its fixture prefix
    ./tests/run --lane node --keep                # leave the daemons running

Design and full specification: https://wiki.satforge.dev/doc/integration-testing-system-tAKjyvbcHo
(M1 scope: node lane, host sandbox, script drivers. VM sandbox, agent
driver, suites, and net-lane execution through this runner arrive in M2+.)

The former repo-root `netsim/` lives here now: `tests/net/` (tasks,
`link.sh`, stories) and `tests/stages/` (`lab` + the bootstrap/adopt chain
recipes) — same netsim workflow, new home; register with
`./tests/net/link.sh`.

- A **scenario** is a directory under `node/scenarios/`: `scenario.toml`
  (start/saves/nodes/drivers/timeout), `script.py` (driver), `verify.py`
  (oracle — the only judge), `README.md`.
- Scenarios chain through named states (`null → one-node → two-nodes`);
  the runner builds exactly the fixture prefix a selection needs.
- Every run writes `results/<stamp>/results.json` + `events.jsonl`
  (schema in `lib/results.py`) plus per-scenario driver/oracle logs and
  each node's astrald log under `results/<stamp>/session/`.
- Requirements: Python ≥ 3.11, Go toolchain, an astral-py checkout
  (path in `config.toml`) — imported directly from its `src/` (the package
  has zero dependencies); no venv or pip involved.

Manifest format note: TOML (stdlib), not YAML as the design doc sketches —
one fewer dependency; to be reconciled in the doc.

## Known issue

`bootstrap-user-software-key` can intermittently fail with
`auth.sign_contract: sign as issuer: unsupported` — a pre-existing astrald
race (the crypto module indexes a stored private key asynchronously; a
`sign_contract` arriving before the index lands finds no signer). Observed
losing by ~26 ms roughly half the time on this host. Not introduced by this
branch (zero daemon changes); re-run until the daemon-side fix lands.
