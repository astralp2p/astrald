# tests/ M1 — Walking Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The thinnest end-to-end slice of the Astral integration testing system: `tests/run` spawns real astrald processes on loopback, script drivers perform the bootstrap and adoption flows, scenario-owned oracles judge them, and one `results.json` reports it — proving runner → session → driver → oracle → results. Plus the structural landing: repo-root `netsim/` migrates into the settled `tests/net/` + `tests/stages/` layout with its workflow intact (goal criterion 6).

**Architecture:** A stdlib Python entry point (`tests/run`) bootstraps a venv with astral-py and re-execs into it. `lib/` splits into pure modules (results, scenario manifests, node-config rendering — unit-testable without astrald) and live modules (localnode, session, runner — they import `astral` and touch real daemons). Scenarios are directories with a TOML manifest, a `script.py` driver, and a `verify.py` oracle, both executed as subprocesses reading `session.json`.

**Tech Stack:** Python ≥ 3.11 (tomllib, asyncio.TaskGroup), astral-py 0.2.0b1 (dist `astral-ipc`, import `astral`), Go 1.25 (builds astrald from this worktree), git.

**Spec:** https://wiki.satforge.dev/doc/implementation-plan-KeRVNel8Pe (M1 section) · design: https://wiki.satforge.dev/doc/integration-testing-system-tAKjyvbcHo

## Global Constraints

- The migration criterion (Task 10) moves `netsim/` INTO `tests/` — `git mv` + path fixes only, no task-internal rewrites. Beyond that move, changes stay inside `tests/`; no unrelated repo files touched (doc path references to the old `netsim/` location are part of the move).
- Only dependency: `astral-ipc` (astral-py 0.2.0b1) — a zero-runtime-dependency package imported DIRECTLY from the sibling checkout via sys.path (config key `astral_py.path`, default `~/work/astralp2p/astral-py/master`). No venv, no pip — the host lacks ensurepip (confirmed 2026-07-29). Everything runs on system python3; `tests/run` is stdlib plus that path injection.
- Manifest format is TOML (`scenario.toml`, `config.toml`) via stdlib `tomllib` — the design doc says YAML; PyYAML would be a second dependency. Flag this deviation in the PR description for a doc sync.
- Two astrald instances on one host collide on five defaults — apphost tcp `8625`, http `8624`, `unix:~/.apphost.sock`, tcp `listen_port` 1791, ether udp `8822`, kcp `listen_port` 1792. Every node gets ALL of these overridden per-root (http disabled, unix socket omitted).
- Every opened astral-py `Client`/`Stream` MUST be `async with`-managed: a leak permanently burns one of astrald's 32 apphost workers.
- Commit after every task. Branch `intern0/dev/tests-m1-skeleton`; never push to master.
- Definition of done = the six goal criteria (five runtime checks + the structural migration), executed on this host in Tasks 10–11, output quoted in the Outline task Log.

## Verified ground facts (used throughout; do not re-derive)

- `astrald -root <dir>` → config in `<dir>/config/` (`node.yaml`, `apphost.yaml`, `tcp.yaml`, `ether.yaml`), key at `<dir>/config/node_key`. Runs foreground; SIGINT = graceful stop. Startup log prints `astral node <alias> (<66-hex>) starting...` — the node identity.
- `apphost.yaml`: `listen` (list; `"tcp:127.0.0.1:PORT"` form), `bind_http: ""` disables HTTP, `tokens: {<token-string>: <identity-or-alias>}` — seeding `localnode` as the value grants that token the NODE's own identity (full local authority).
- `tcp.yaml`: `listen_port: <int>` (default 1791). `ether.yaml`: `udp_port: <int>` (default 8822) — distinct per node prevents cross-talk; `node.yaml` `modules:` is an allowlist (omit = all modules), so we do NOT use it.
- Readiness probe: connect apphost (`tcp:127.0.0.1:<port>`), call `client.shell.spec()` → non-empty list[OpSpec]. Anonymous is allowed for outbound queries by default.
- Explicit loopback link (no LAN discovery): `nodes.add_endpoint?id=<66hex>&endpoint=tcp:127.0.0.1:<port>` on both sides, then `nodes.new_link?target=<66hex>&endpoint=tcp:127.0.0.1:<port>&strategies=basic`.
- Bootstrap op chain (all as the node-token client): `bip137sig.new_entropy` → (stream) `bip137sig.mnemonic` → (stream) `bip137sig.seed` → (stream) `bip137sig.derive_key?path=m/44'/0'/0'/0/0` → `objects.store` (private key; auto-indexes signer) → (stream) `crypto.public_key` = User identity → `user.new_node_contract?user=<hex>` → (stream) `auth.sign_contract` → (stream) `user.accept_contract` → `apphost.create_token?id=<hex>` → AccessToken.
- Adoption: as the User (token from bootstrap) on node1: `client.user.adopt(<node2 hex>)` → SignedContract. Verify (ported from netsim adopt-node verify.py): node1 `user.info` issuer == User; node2 `user.info` issuer == same User; both `user.swarm_status` list the other as the linked sibling; node2 `nodes.links` shows a link back to node1.
- astral-py API: `async with await astral.connect(endpoint, token=...) as c:`; generic `await c.call_one(qs, **kw)`, `await c.call_with(qs, *objects, **kw) -> list`, typed helpers `c.shell.spec()`, `c.apphost.whoami() -> Identity`, `c.apphost.create_token(id) -> AccessToken(.token)`, `c.user.info() -> Info(.user_id, .node_id, .contract)`, `c.user.swarm_status() -> list[SwarmMember(identity, alias, linked)]`, `c.user.adopt(node) -> SignedContract`, `c.user.accept_contract(signed)`, `c.dir.set_alias(identity, alias)`, `c.objects.store(*objs) -> list[ObjectID]`. Identity args accept 66-hex strings; `str(Identity)` is the hex.

## File Structure

```
tests/
├── run                        # stdlib entry: astral-py sys.path injection, then lib.runner.main()
├── config.toml                # ports.base, astral_py.path, build.package
├── .gitignore                 # .cache/ results/ __pycache__/
├── lib/
│   ├── __init__.py            # empty
│   ├── results.py             # PURE: RunResults writer, schema, exit code
│   ├── scenarios.py           # PURE: manifest load, chain resolution, selection
│   ├── nodeconfig.py          # PURE: render per-root YAML config strings
│   ├── sessionio.py           # PURE: drivers/oracles read session.json + write facts
│   ├── localnode.py           # LIVE: spawn astrald, parse identity, await ready, stop
│   ├── session.py             # LIVE: roster, ports, session.json, facts merge, liveness
│   └── runner.py              # LIVE: build astrald, execute chain, classify failures
├── selftest/                  # unit tests for the PURE modules (python3 -m unittest)
│   ├── test_results.py
│   ├── test_scenarios.py
│   └── test_nodeconfig.py
└── node/scenarios/
    ├── smoke/                     # start null · nodes [node1]
    ├── bootstrap-user-software-key/   # start null → saves one-node · nodes [node1]
    └── adopt-node/                # start one-node → saves two-nodes · nodes [node1,node2]
        ├── scenario.toml
        ├── script.py              # driver
        ├── verify.py              # oracle
        └── README.md
```

Runtime layout (gitignored): `tests/.cache/astrald` (built binary), `tests/.venv/`, `tests/results/<stamp>/{results.json, events.jsonl, session/<node>/..., node/<scenario>/{driver.log,verify.log,facts.json}}`.

Post-migration (Task 10) the tree additionally holds: `tests/net/` (tasks incl. `_lib/`, `link.sh`, README, substrate + not-yet-descended scenarios) and `tests/stages/` (the `lab`, `bootstrap-user-software-key`, `adopt-node` stage-chain recipes) — all moved from repo-root `netsim/`.

---

### Task 1: Scaffold + entry point with astral-py path injection

**Files:**
- Create: `tests/run` (mode 755), `tests/config.toml`, `tests/.gitignore`, `tests/lib/__init__.py`, `tests/selftest/__init__.py`

**Interfaces:**
- Produces: `tests/run [--lane node] [--only NAME ...] [--keep]`; env `ASTRAL_TESTS_PYPATH` (tests dir + astral-py `src`, os.pathsep-joined) exported for scenario subprocesses. `lib.runner.main(args) -> int` (stub until Task 7).

- [ ] **Step 1: Write the files**

`tests/.gitignore`:
```gitignore
.cache/
results/
__pycache__/
```

`tests/config.toml`:
```toml
[ports]
base = 20800            # node N uses base+10N+0 apphost, +1 astral tcp, +2 ether udp

[astral_py]
path = "~/work/astralp2p/astral-py/master"

[build]
package = "./cmd/astrald"
```

`tests/run`:
```python
#!/usr/bin/env python3
"""tests/run — entry point of the integration test system (M1: node lane only).

Stdlib only. astral-py is a zero-dependency package, so it is imported
straight from its checkout (config.toml astral_py.path) via sys.path —
no venv, no pip (the host lacks ensurepip).
"""
import argparse
import os
import sys
import tomllib
from pathlib import Path

TESTS = Path(__file__).resolve().parent


def astral_src() -> Path:
    cfg = tomllib.loads((TESTS / "config.toml").read_text())
    src = Path(os.path.expanduser(cfg["astral_py"]["path"])) / "src"
    if not (src / "astral" / "__init__.py").exists():
        sys.exit(f"run: astral-py not found at {src} (config.toml astral_py.path)")
    return src


def main() -> int:
    ap = argparse.ArgumentParser(prog="tests/run")
    ap.add_argument("--lane", choices=["node"], default="node")
    ap.add_argument("--only", nargs="*", default=None, metavar="SCENARIO")
    ap.add_argument("--keep", action="store_true", help="leave the daemons running")
    args = ap.parse_args()

    if sys.version_info < (3, 11):
        sys.exit("run: Python >= 3.11 required")

    src = astral_src()
    sys.path.insert(0, str(src))
    sys.path.insert(0, str(TESTS))
    os.environ["ASTRAL_TESTS_PYPATH"] = f"{TESTS}{os.pathsep}{src}"

    from lib import runner
    return runner.main(args)


if __name__ == "__main__":
    sys.exit(main())
```

`tests/lib/__init__.py` and `tests/selftest/__init__.py`: empty files.

Stub `tests/lib/runner.py` (replaced in Task 7):
```python
"""Runner stub — replaced by the real orchestration in a later task."""


def main(args) -> int:
    print(f"runner stub: lane={args.lane} only={args.only} keep={args.keep}")
    return 0
```

- [ ] **Step 2: Verify the skeleton runs and astral imports via the path**

Run: `cd /home/intern0/work/astralp2p/astrald/dev--tests-m1-skeleton && chmod +x tests/run && ./tests/run --lane node`
Expected: `runner stub: lane=node only=None keep=False`.
Then: `PYTHONPATH="$HOME/work/astralp2p/astral-py/master/src" python3 -c "import astral; print('astral import OK')"`
Expected: `astral import OK`.

- [ ] **Step 3: Commit**

```bash
git add tests/run tests/config.toml tests/.gitignore tests/lib/__init__.py tests/selftest/__init__.py tests/lib/runner.py
git commit -m "feat(tests): M1 scaffold — entry point with astral-py path injection"
```

---

### Task 2: lib/results.py — the stable output

**Files:**
- Create: `tests/lib/results.py`
- Test: `tests/selftest/test_results.py`

**Interfaces:**
- Produces: `RunResults(dir: Path, astrald_ref: str, host: str, sandbox: str, lanes: list[str])`; `.record(test, kind, lane, driver, status, duration_s, artifacts, failure_kind=None)` (validates, appends to `events.jsonl` immediately); `.finalize() -> int` (writes `results.json`, returns process exit code: 0 iff every entry `status == "pass"`). Constants `STATUSES = {"pass","fail","skipped"}`, `FAILURE_KINDS = {"driver","verify","environment"}`, `KINDS = {"test","fixture"}`.

- [ ] **Step 1: Write the failing test**

`tests/selftest/test_results.py`:
```python
import json
import tempfile
import unittest
from pathlib import Path

from lib.results import RunResults


class TestResults(unittest.TestCase):
    def make(self, tmp):
        return RunResults(Path(tmp), astrald_ref="abc1234", host="pi",
                          sandbox="host", lanes=["node"])

    def test_pass_run_exits_zero_and_writes_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            r.record(test="node/smoke", kind="test", lane="node", driver="script",
                     status="pass", duration_s=1.2, artifacts="node/smoke/")
            self.assertEqual(r.finalize(), 0)
            data = json.loads((Path(tmp) / "results.json").read_text())
            for key in ("astrald_ref", "host", "sandbox", "lanes", "hermetic",
                        "started", "wall_time_s", "entries"):
                self.assertIn(key, data)
            self.assertEqual(data["astrald_ref"], "abc1234")
            self.assertEqual(data["host"], "pi")
            self.assertEqual(data["sandbox"], "host")
            self.assertEqual(data["lanes"], ["node"])
            self.assertTrue(data["hermetic"])
            self.assertEqual(len(data["entries"]), 1)
            self.assertEqual(data["entries"][0]["status"], "pass")
            self.assertNotIn("failure_kind", data["entries"][0])
            lines = (Path(tmp) / "events.jsonl").read_text().splitlines()
            self.assertEqual(json.loads(lines[0])["test"], "node/smoke")

    def test_fail_and_skip_exit_nonzero(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            r.record(test="node/bootstrap-user-software-key", kind="fixture",
                     lane="node", driver="script", status="fail",
                     failure_kind="verify", duration_s=3.0,
                     artifacts="node/bootstrap-user-software-key/")
            r.record(test="node/adopt-node", kind="test", lane="node",
                     driver="script", status="skipped", duration_s=0.0,
                     artifacts="node/adopt-node/")
            self.assertEqual(r.finalize(), 1)
            entries = json.loads((Path(tmp) / "results.json").read_text())["entries"]
            self.assertEqual(entries[0]["failure_kind"], "verify")
            self.assertNotIn("failure_kind", entries[1])
            events = [json.loads(l) for l in
                      (Path(tmp) / "events.jsonl").read_text().splitlines()]
            self.assertEqual(events[0]["failure_kind"], "verify")
            self.assertNotIn("failure_kind", events[1])

    def test_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            with self.assertRaises(ValueError):
                r.record(test="x", kind="test", lane="node", driver="script",
                         status="nope", duration_s=0, artifacts="x/")
            with self.assertRaises(ValueError):   # fail requires failure_kind
                r.record(test="x", kind="test", lane="node", driver="script",
                         status="fail", duration_s=0, artifacts="x/")


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd tests && ../tests/.venv/bin/python3 -m unittest selftest.test_results -v 2>&1 | head -5`
(Equivalently `PYTHONPATH=tests python3 -m unittest ...` from repo root — pure module, system python works too.)
Expected: `ModuleNotFoundError: No module named 'lib.results'`

- [ ] **Step 3: Implement**

`tests/lib/results.py`:
```python
"""results.json / events.jsonl writer — the system's one stable output."""
import json
import time
from pathlib import Path

STATUSES = {"pass", "fail", "skipped"}
FAILURE_KINDS = {"driver", "verify", "environment"}
KINDS = {"test", "fixture"}


class RunResults:
    def __init__(self, dir: Path, astrald_ref: str, host: str,
                 sandbox: str, lanes: list):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self._t0 = time.monotonic()
        self.header = {
            "astrald_ref": astrald_ref,
            "host": host,
            "sandbox": sandbox,
            "lanes": list(lanes),
            "hermetic": True,
            "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "wall_time_s": 0.0,
        }
        self.entries = []

    def record(self, *, test: str, kind: str, lane: str, driver: str,
               status: str, duration_s: float, artifacts: str,
               failure_kind: str | None = None) -> None:
        if status not in STATUSES:
            raise ValueError(f"bad status {status!r}")
        if kind not in KINDS:
            raise ValueError(f"bad kind {kind!r}")
        if status == "fail":
            if failure_kind not in FAILURE_KINDS:
                raise ValueError(f"fail requires failure_kind, got {failure_kind!r}")
        elif failure_kind is not None:
            raise ValueError("failure_kind only valid on fail")
        entry = {"test": test, "kind": kind, "lane": lane, "driver": driver,
                 "status": status, "duration_s": round(duration_s, 3),
                 "artifacts": artifacts}
        if failure_kind:
            entry["failure_kind"] = failure_kind
        self.entries.append(entry)
        with (self.dir / "events.jsonl").open("a") as f:
            f.write(json.dumps(entry) + "\n")

    def finalize(self) -> int:
        self.header["wall_time_s"] = round(time.monotonic() - self._t0, 3)
        doc = dict(self.header, entries=self.entries)
        (self.dir / "results.json").write_text(json.dumps(doc, indent=2) + "\n")
        return 0 if all(e["status"] == "pass" for e in self.entries) else 1
```

- [ ] **Step 4: Run to verify it passes**

Run: `PYTHONPATH=tests python3 -m unittest selftest.test_results -v`
Expected: `OK` (3 tests)

- [ ] **Step 5: Commit**

```bash
git add tests/lib/results.py tests/selftest/test_results.py
git commit -m "feat(tests): results writer — schema, events stream, exit code"
```

---

### Task 3: lib/scenarios.py — manifests and chain resolution

**Files:**
- Create: `tests/lib/scenarios.py`
- Test: `tests/selftest/test_scenarios.py`

**Interfaces:**
- Produces: `Scenario` dataclass: `name, dir: Path, start: str, saves: str|None, nodes: list[str], drivers: list[str], timeout: int`. `load_all(scenarios_dir: Path) -> dict[str, Scenario]` (reads every `*/scenario.toml`). `resolve(all: dict, only: list[str]|None) -> list[tuple[Scenario, str]]` — execution order with kind `"test"` (selected) or `"fixture"` (chain prerequisite); no selection = everything is a test. Raises `ValueError` on unknown `--only` name, unknown `start` state, or a `start` cycle.
- State model: `start` names a state; `"null"` is the empty state; a scenario's `saves` names the state it produces. Order = ascending chain depth (`null`=0), ties by name.

- [ ] **Step 1: Write the failing test**

`tests/selftest/test_scenarios.py`:
```python
import tempfile
import textwrap
import unittest
from pathlib import Path

from lib.scenarios import load_all, resolve


def write_scenario(root, name, toml):
    d = Path(root) / name
    d.mkdir(parents=True)
    (d / "scenario.toml").write_text(textwrap.dedent(toml))


class TestScenarios(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = self.tmp.name
        write_scenario(root, "smoke", """
            start = "null"
            nodes = ["node1"]
            drivers = ["script"]
            timeout = 60
        """)
        write_scenario(root, "bootstrap-user-software-key", """
            start = "null"
            saves = "one-node"
            nodes = ["node1"]
            drivers = ["script"]
            timeout = 120
        """)
        write_scenario(root, "adopt-node", """
            start = "one-node"
            saves = "two-nodes"
            nodes = ["node1", "node2"]
            drivers = ["script"]
            timeout = 180
        """)
        self.all = load_all(Path(root))

    def tearDown(self):
        self.tmp.cleanup()

    def test_load(self):
        self.assertEqual(set(self.all), {"smoke", "bootstrap-user-software-key",
                                         "adopt-node"})
        self.assertEqual(self.all["adopt-node"].start, "one-node")
        self.assertEqual(self.all["smoke"].saves, None)

    def test_full_run_order_and_kinds(self):
        plan = resolve(self.all, only=None)
        names = [s.name for s, _ in plan]
        self.assertEqual(names, ["bootstrap-user-software-key", "smoke",
                                 "adopt-node"])
        self.assertTrue(all(kind == "test" for _, kind in plan))

    def test_only_builds_fixture_prefix(self):
        plan = resolve(self.all, only=["adopt-node"])
        self.assertEqual([(s.name, k) for s, k in plan],
                         [("bootstrap-user-software-key", "fixture"),
                          ("adopt-node", "test")])

    def test_errors(self):
        with self.assertRaises(ValueError):
            resolve(self.all, only=["no-such"])
        bad = dict(self.all)
        bad["orphan"] = bad["adopt-node"].__class__(
            name="orphan", dir=Path("."), start="missing-state", saves=None,
            nodes=["node1"], drivers=["script"], timeout=1)
        with self.assertRaises(ValueError):
            resolve(bad, only=["orphan"])

    def test_unrelated_broken_scenario_does_not_break_selection(self):
        write_scenario(self.tmp.name, "broken", """
            start = "missing-state"
            nodes = ["node1"]
        """)
        all2 = load_all(Path(self.tmp.name))
        plan = resolve(all2, only=["smoke"])
        self.assertEqual([(s.name, k) for s, k in plan], [("smoke", "test")])
        with self.assertRaises(ValueError):
            resolve(all2, only=["broken"])

    def test_cycle_detected(self):
        write_scenario(self.tmp.name, "cyc-a", """
            start = "state-b"
            saves = "state-a"
            nodes = ["node1"]
        """)
        write_scenario(self.tmp.name, "cyc-b", """
            start = "state-a"
            saves = "state-b"
            nodes = ["node1"]
        """)
        with self.assertRaises(ValueError):
            resolve(load_all(Path(self.tmp.name)), only=["cyc-a"])


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run to verify it fails**

Run: `PYTHONPATH=tests python3 -m unittest selftest.test_scenarios -v 2>&1 | head -3`
Expected: `ModuleNotFoundError: No module named 'lib.scenarios'`

- [ ] **Step 3: Implement**

`tests/lib/scenarios.py`:
```python
"""Scenario manifests (scenario.toml) and start/saves chain resolution."""
import tomllib
from dataclasses import dataclass
from pathlib import Path


@dataclass
class Scenario:
    name: str
    dir: Path
    start: str
    saves: str | None
    nodes: list
    drivers: list
    timeout: int


def load_all(scenarios_dir: Path) -> dict:
    out = {}
    for mf in sorted(scenarios_dir.glob("*/scenario.toml")):
        raw = tomllib.loads(mf.read_text())
        name = mf.parent.name
        out[name] = Scenario(
            name=name, dir=mf.parent,
            start=raw.get("start", "null"),
            saves=raw.get("saves") or None,
            nodes=list(raw.get("nodes", [])),
            drivers=list(raw.get("drivers", ["script"])),
            timeout=int(raw.get("timeout", 120)),
        )
    return out


def resolve(all: dict, only) -> list:
    """Execution plan for a selection. Validation is scoped to the scenarios
    the selection actually reaches — an unrelated broken manifest must never
    break a targeted --only run."""
    producers = {s.saves: s for s in all.values() if s.saves}

    def depth_of(state: str, seen=()) -> int:
        if state == "null":
            return 0
        if state in seen:
            raise ValueError(f"start/saves cycle at state {state!r}")
        if state not in producers:
            raise ValueError(f"no scenario saves state {state!r}")
        return 1 + depth_of(producers[state].start, seen + (state,))

    if only is None:
        selected = set(all)
    else:
        unknown = [n for n in only if n not in all]
        if unknown:
            raise ValueError(f"unknown scenario(s): {', '.join(unknown)}")
        selected = set(only)

    needed = set(selected)
    frontier = list(selected)
    depths = {}
    while frontier:
        s = all[frontier.pop()]
        depths[s.name] = depth_of(s.start)
        if s.start != "null":
            dep = producers[s.start].name
            if dep not in needed:
                needed.add(dep)
                frontier.append(dep)

    ordered = sorted(needed, key=lambda n: (depths[n], n))
    return [(all[n], "test" if n in selected else "fixture") for n in ordered]
```

- [ ] **Step 4: Run to verify it passes**

Run: `PYTHONPATH=tests python3 -m unittest selftest.test_scenarios -v`
Expected: `OK` (6 tests)

- [ ] **Step 5: Commit**

```bash
git add tests/lib/scenarios.py tests/selftest/test_scenarios.py
git commit -m "feat(tests): scenario manifests and start/saves chain resolution"
```

---

### Task 4: lib/nodeconfig.py — per-root astrald config rendering

**Files:**
- Create: `tests/lib/nodeconfig.py`
- Test: `tests/selftest/test_nodeconfig.py`

**Interfaces:**
- Produces: `NodePorts` dataclass `(apphost: int, tcp: int, ether: int, kcp: int)`; `ports_for(base: int, index: int) -> NodePorts` (`base+10*index+{0,1,2,3}`); `render(root: Path, ports: NodePorts, token: str) -> None` — writes `<root>/config/{apphost.yaml,tcp.yaml,ether.yaml,kcp.yaml}`. No `astral` import (pure). kcp's default `listen_port` 1792 (mod/kcp/src/config.go:22) collides on a shared host exactly like tcp's 1791 — it is parameterized for the same reason.
- Consumed by: Task 5 `LocalNode`.

- [ ] **Step 1: Write the failing test**

`tests/selftest/test_nodeconfig.py`:
```python
import tempfile
import unittest
from pathlib import Path

from lib.nodeconfig import NodePorts, ports_for, render


class TestNodeConfig(unittest.TestCase):
    def test_ports_for(self):
        p = ports_for(20800, 1)
        self.assertEqual((p.apphost, p.tcp, p.ether, p.kcp),
                         (20810, 20811, 20812, 20813))

    def test_render(self):
        with tempfile.TemporaryDirectory() as tmp:
            render(Path(tmp), NodePorts(20800, 20801, 20802, 20803),
                   token="sekrit")
            cfg = Path(tmp) / "config"
            apphost = (cfg / "apphost.yaml").read_text()
            self.assertIn('- "tcp:127.0.0.1:20800"', apphost)
            self.assertIn('bind_http: ""', apphost)
            self.assertIn('"sekrit": localnode', apphost)
            self.assertNotIn("unix:", apphost)      # no socket-path collisions
            self.assertIn("listen_port: 20801", (cfg / "tcp.yaml").read_text())
            self.assertIn("udp_port: 20802", (cfg / "ether.yaml").read_text())
            self.assertIn("listen_port: 20803", (cfg / "kcp.yaml").read_text())


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run to verify it fails**

Run: `PYTHONPATH=tests python3 -m unittest selftest.test_nodeconfig -v 2>&1 | head -3`
Expected: `ModuleNotFoundError: No module named 'lib.nodeconfig'`

- [ ] **Step 3: Implement**

`tests/lib/nodeconfig.py`:
```python
"""Render an isolated astrald root's config files (loopback, collision-free).

Every default that two instances would fight over is overridden here:
apphost tcp 8625 / http 8624 / unix socket, tcp listen_port 1791, ether udp
8822, kcp listen_port 1792. ether/kcp get distinct ports instead of a
modules: allowlist — the allowlist would need every module named and
silently rot as astrald grows.
"""
from dataclasses import dataclass
from pathlib import Path

APPHOST_YAML = """\
listen:
  - "tcp:127.0.0.1:{apphost}"
bind_http: ""
tokens:
  "{token}": localnode
"""

TCP_YAML = """\
listen_port: {tcp}
"""

ETHER_YAML = """\
udp_port: {ether}
"""

KCP_YAML = """\
listen_port: {kcp}
"""


@dataclass
class NodePorts:
    apphost: int
    tcp: int
    ether: int
    kcp: int


def ports_for(base: int, index: int) -> NodePorts:
    p = base + 10 * index
    return NodePorts(apphost=p, tcp=p + 1, ether=p + 2, kcp=p + 3)


def render(root: Path, ports: NodePorts, token: str) -> None:
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "apphost.yaml").write_text(
        APPHOST_YAML.format(apphost=ports.apphost, token=token))
    (cfg / "tcp.yaml").write_text(TCP_YAML.format(tcp=ports.tcp))
    (cfg / "ether.yaml").write_text(ETHER_YAML.format(ether=ports.ether))
    (cfg / "kcp.yaml").write_text(KCP_YAML.format(kcp=ports.kcp))
```

- [ ] **Step 4: Run to verify it passes**

Run: `PYTHONPATH=tests python3 -m unittest selftest -v`
Expected: `OK` (all selftests so far)

- [ ] **Step 5: Commit**

```bash
git add tests/lib/nodeconfig.py tests/selftest/test_nodeconfig.py
git commit -m "feat(tests): per-root astrald config rendering, collision-free ports"
```

---

### Task 5: lib/localnode.py + astrald build — one real node up and ready

**Files:**
- Create: `tests/lib/localnode.py`, `tests/lib/build.py`

**Interfaces:**
- Produces: `build.ensure_binary(repo_root: Path) -> tuple[Path, str]` — `go build -o tests/.cache/astrald ./cmd/astrald`, returns `(binary_path, short_ref)`. `LocalNode(name, root: Path, binary: Path, ports: NodePorts)` with attrs `.token` (random hex), `.identity` (66-hex str, set by `wait_ready`), `.endpoint` (`tcp:127.0.0.1:<apphost>`), `.log_path`; methods `.start()`, `async .wait_ready(timeout=30.0)` (raises `NodeError` on timeout/death), `.stop()`, `.alive() -> bool`.
- Consumes: `nodeconfig.render`, `nodeconfig.NodePorts`.

- [ ] **Step 1: Implement**

`tests/lib/build.py`:
```python
"""Build astrald from this worktree; the binary under test."""
import subprocess
from pathlib import Path


def ensure_binary(repo_root: Path) -> tuple:
    cache = repo_root / "tests" / ".cache"
    cache.mkdir(parents=True, exist_ok=True)
    binary = cache / "astrald"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/astrald"],
                   cwd=repo_root, check=True)
    ref = subprocess.run(["git", "rev-parse", "--short", "HEAD"],
                         cwd=repo_root, check=True, capture_output=True,
                         text=True).stdout.strip()
    return binary, ref
```

`tests/lib/localnode.py`:
```python
"""Spawn and manage one isolated astrald instance on loopback."""
import asyncio
import re
import secrets
import signal
import subprocess
from pathlib import Path

import astral

from lib.nodeconfig import NodePorts, render

# startup line prints the identity in parens; astrald's earliest lines are
# raw %v dumps, so the hex can carry internal padding: "( <66-hex> )"
_IDENT_RE = re.compile(r"\(\s*([0-9a-f]{66})\s*\)")


class NodeError(Exception):
    pass


class LocalNode:
    def __init__(self, name: str, root: Path, binary: Path, ports: NodePorts):
        self.name = name
        self.root = Path(root)
        self.binary = Path(binary)
        self.ports = ports
        self.token = secrets.token_hex(16)
        self.identity = None
        self.proc = None
        self.log_path = self.root / "astrald.log"

    @property
    def endpoint(self) -> str:
        return f"tcp:127.0.0.1:{self.ports.apphost}"

    def start(self) -> None:
        render(self.root, self.ports, self.token)
        self.root.mkdir(parents=True, exist_ok=True)
        log = self.log_path.open("ab")
        self.proc = subprocess.Popen(
            [str(self.binary), "-root", str(self.root)],
            stdout=log, stderr=subprocess.STDOUT)

    def alive(self) -> bool:
        return self.proc is not None and self.proc.poll() is None

    def _scrape_identity(self) -> None:
        if self.identity is None and self.log_path.exists():
            m = _IDENT_RE.search(self.log_path.read_text(errors="replace"))
            if m:
                self.identity = m.group(1)

    async def wait_ready(self, timeout: float = 30.0) -> None:
        deadline = asyncio.get_event_loop().time() + timeout
        last = None
        while asyncio.get_event_loop().time() < deadline:
            if not self.alive():
                raise NodeError(f"{self.name}: astrald exited "
                                f"rc={self.proc.returncode}; see {self.log_path}")
            self._scrape_identity()
            if self.identity is not None:
                try:
                    async with await astral.connect(
                            self.endpoint, token=self.token,
                            connect_timeout=2.0) as c:
                        specs = await c.shell.spec()
                    if specs:
                        return
                except astral.AstralError as e:
                    last = e
            await asyncio.sleep(0.5)
        raise NodeError(f"{self.name}: not ready after {timeout}s "
                        f"(last error: {last}); see {self.log_path}")

    def stop(self) -> None:
        if self.proc is None or self.proc.poll() is not None:
            return
        self.proc.send_signal(signal.SIGINT)
        try:
            self.proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)
```

- [ ] **Step 2: Live-verify — one node spawns, answers `shell.spec`, stops**

Run (from the worktree root):
```bash
python3 - <<'EOF'
import asyncio, os, sys, tempfile, tomllib
from pathlib import Path
sys.path.insert(0, "tests")
_src = Path(os.path.expanduser(tomllib.loads(Path("tests/config.toml").read_text())["astral_py"]["path"])) / "src"
sys.path.insert(0, str(_src))
from lib.build import ensure_binary
from lib.localnode import LocalNode
from lib.nodeconfig import ports_for

async def main():
    binary, ref = ensure_binary(Path("."))
    print("built", ref)
    with tempfile.TemporaryDirectory() as tmp:
        n = LocalNode("probe", Path(tmp) / "probe", binary, ports_for(21800, 0))
        n.start()
        try:
            await n.wait_ready()
            print("READY identity:", n.identity, "endpoint:", n.endpoint)
        finally:
            n.stop()          # never leak the daemon, even on a failed wait
    print("STOPPED cleanly")

asyncio.run(main())
EOF
```
Expected: `built <ref>`, `READY identity: 02..|03.. (66 hex)`, `STOPPED cleanly`. If `wait_ready` times out, read the printed log path — this is the M1 risk gate; report findings rather than patching around a daemon-side problem.

- [ ] **Step 3: Commit**

```bash
git add tests/lib/localnode.py tests/lib/build.py
git commit -m "feat(tests): localnode — build, spawn, readiness, teardown"
```

---

### Task 6: lib/session.py + lib/sessionio.py — the multi-node session

**Files:**
- Create: `tests/lib/session.py`, `tests/lib/sessionio.py`

**Interfaces:**
- Produces: `Session(dir: Path, binary: Path, port_base: int)`; `async .ensure(names: list[str])` (spawns missing nodes, index = roster order, awaits readiness); `.nodes: dict[str, LocalNode]`; `.facts: dict`; `.write_session_json()` → `<dir>/session.json` `{"nodes": {name: {endpoint, token, identity, root, tcp_port}}, "facts": {...}}`; `.merge_facts(path: Path)` (reads a driver's facts JSON if present, updates `.facts`); `.dead_nodes() -> list[str]`; `.teardown()`.
- `sessionio.load() -> dict` — for drivers/oracles: reads path from env `ASTRAL_TESTS_SESSION`. `sessionio.write_facts(d: dict)` — writes JSON to env `ASTRAL_TESTS_FACTS_OUT`.

- [ ] **Step 1: Implement**

`tests/lib/sessionio.py`:
```python
"""What drivers and oracles are allowed to see: session.json in, facts out."""
import json
import os
from pathlib import Path


def load() -> dict:
    return json.loads(Path(os.environ["ASTRAL_TESTS_SESSION"]).read_text())


def write_facts(d: dict) -> None:
    out = os.environ.get("ASTRAL_TESTS_FACTS_OUT")
    if out:
        Path(out).write_text(json.dumps(d, indent=2) + "\n")
```

`tests/lib/session.py`:
```python
"""A session: named astrald nodes on loopback + accumulated facts."""
import json
from pathlib import Path

from lib.localnode import LocalNode
from lib.nodeconfig import ports_for


class Session:
    def __init__(self, dir: Path, binary: Path, port_base: int):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.binary = binary
        self.port_base = port_base
        self.nodes = {}
        self.facts = {}

    async def ensure(self, names) -> None:
        for name in names:
            if name in self.nodes:
                continue
            idx = len(self.nodes)
            node = LocalNode(name, self.dir / name, self.binary,
                             ports_for(self.port_base, idx))
            node.start()
            self.nodes[name] = node   # track BEFORE readiness — a node that
            await node.wait_ready()   # fails wait_ready must stay stoppable
        self.write_session_json()

    @property
    def session_json_path(self) -> Path:
        return self.dir / "session.json"

    def write_session_json(self) -> None:
        doc = {"nodes": {}, "facts": self.facts}
        for name, n in self.nodes.items():
            doc["nodes"][name] = {
                "endpoint": n.endpoint,
                "token": n.token,
                "identity": n.identity,
                "root": str(n.root),
                "tcp_port": n.ports.tcp,
            }
        self.session_json_path.write_text(json.dumps(doc, indent=2) + "\n")

    def merge_facts(self, path: Path) -> None:
        if Path(path).exists():
            self.facts.update(json.loads(Path(path).read_text()))
            self.write_session_json()

    def dead_nodes(self):
        return [name for name, n in self.nodes.items() if not n.alive()]

    def teardown(self) -> None:
        for n in self.nodes.values():
            n.stop()
```

- [ ] **Step 2: Live-verify — two nodes concurrently, distinct identities**

```bash
python3 - <<'EOF'
import asyncio, json, os, sys, tempfile, tomllib
from pathlib import Path
sys.path.insert(0, "tests")
_src = Path(os.path.expanduser(tomllib.loads(Path("tests/config.toml").read_text())["astral_py"]["path"])) / "src"
sys.path.insert(0, str(_src))
from lib.build import ensure_binary
from lib.session import Session

async def main():
    binary, _ = ensure_binary(Path("."))
    with tempfile.TemporaryDirectory() as tmp:
        s = Session(Path(tmp), binary, 21900)
        try:
            await s.ensure(["node1", "node2"])
            doc = json.loads(s.session_json_path.read_text())
            ids = {v["identity"] for v in doc["nodes"].values()}
            assert len(ids) == 2 and all(i and len(i) == 66 for i in ids), ids
            assert s.dead_nodes() == []
            print("TWO NODES READY", ids)
        finally:
            s.teardown()

asyncio.run(main())
EOF
```
Expected: `TWO NODES READY {…two distinct 66-hex…}`.

- [ ] **Step 3: Commit**

```bash
git add tests/lib/session.py tests/lib/sessionio.py
git commit -m "feat(tests): session — multi-node roster, session.json, facts"
```

---

### Task 7: lib/runner.py + smoke scenario — the spine closes

**Files:**
- Create: `tests/node/scenarios/smoke/{scenario.toml,script.py,verify.py,README.md}`
- Modify: `tests/lib/runner.py` (replace the stub entirely)

**Interfaces:**
- Consumes: everything above.
- Produces: `runner.main(args) -> int`. Execution contract per scenario: run `script.py` then `verify.py` as subprocesses (venv python), cwd = scenario dir, env `ASTRAL_TESTS_SESSION`, `ASTRAL_TESTS_FACTS_OUT`, `PYTHONPATH=<tests dir>`, stdout+stderr → `results/<stamp>/node/<scenario>/{driver.log,verify.log}`. Classification: driver rc≠0 → `fail/driver`; oracle rc≠0 → `fail/verify`; **if any session node died, either overrides to `fail/environment`**; spawn/readiness errors → `fail/environment`; downstream chain entries → `skipped`. Session dirs live under `results/<stamp>/session/` (so node logs are artifacts automatically); torn down at exit, kept with `--keep` or on failure.

- [ ] **Step 1: Write the smoke scenario**

`tests/node/scenarios/smoke/scenario.toml`:
```toml
start = "null"
nodes = ["node1"]
drivers = ["script"]
timeout = 60
```

`tests/node/scenarios/smoke/script.py`:
```python
#!/usr/bin/env python3
"""Driver: touch the node anonymously — the cheapest possible real query."""
import asyncio

import astral

from lib.sessionio import load


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"]) as c:      # anonymous
        specs = await c.shell.spec()
        assert specs, "empty op catalog"
        print(f"driver: node1 exposes {len(specs)} ops")


asyncio.run(main())
```

`tests/node/scenarios/smoke/verify.py`:
```python
#!/usr/bin/env python3
"""Oracle: the node answers, authenticates, and exposes the core ops."""
import asyncio

import astral

from lib.sessionio import load


async def main():
    n1 = load()["nodes"]["node1"]

    async with await astral.connect(n1["endpoint"]) as c:      # anonymous
        names = {s.name for s in await c.shell.spec()}
    for op in ("user.info", "dir.set_alias", "nodes.add_endpoint"):
        assert op in names, f"missing op {op}"

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        assert c.authenticated, "node token not accepted"
        who = await c.apphost.whoami()
        assert str(who) == n1["identity"], (
            f"whoami {who} != node identity {n1['identity']}")
    print("oracle: spec + auth + whoami ok")


asyncio.run(main())
```

`tests/node/scenarios/smoke/README.md`:
```markdown
# smoke

Start state: null (one fresh node). The driver queries the op catalog
anonymously; the oracle checks the catalog carries the core ops, the seeded
node token authenticates, and `apphost.whoami` returns the node identity.
```

- [ ] **Step 2: Implement the runner**

`tests/lib/runner.py` (full replacement):
```python
"""Orchestration: build, resolve chain, execute scenarios, write results."""
import asyncio
import os
import platform
import subprocess
import sys
import time
import tomllib
from pathlib import Path

from lib import scenarios as scn
from lib.build import ensure_binary
from lib.results import RunResults
from lib.session import Session

TESTS = Path(__file__).resolve().parent.parent
REPO = TESTS.parent


def _run_step(py: str, script: Path, log: Path, env: dict, timeout: int) -> int:
    with log.open("wb") as f:
        try:
            p = subprocess.run([py, str(script)], cwd=script.parent,
                               stdout=f, stderr=subprocess.STDOUT,
                               env=env, timeout=timeout)
            return p.returncode
        except subprocess.TimeoutExpired:
            f.write(b"\n[runner] step timed out\n")
            return 124


def main(args) -> int:
    cfg = tomllib.loads((TESTS / "config.toml").read_text())
    binary, ref = ensure_binary(REPO)

    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    run_dir = TESTS / "results" / stamp
    results = RunResults(run_dir, astrald_ref=ref, host=platform.node(),
                         sandbox="host", lanes=[args.lane])

    all_scn = scn.load_all(TESTS / "node" / "scenarios")
    try:
        plan = scn.resolve(all_scn, args.only)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2

    session = Session(run_dir / "session", binary, cfg["ports"]["base"])
    py = sys.executable
    base_env = dict(os.environ,
                    PYTHONPATH=os.environ.get("ASTRAL_TESTS_PYPATH", str(TESTS)),
                    ASTRAL_TESTS_SESSION=str(session.session_json_path))

    failed_states = set()
    any_failed = False
    try:
        for s, kind in plan:
            art = run_dir / "node" / s.name
            art.mkdir(parents=True, exist_ok=True)
            entry = dict(test=f"node/{s.name}", kind=kind, lane="node",
                         driver="script", artifacts=f"node/{s.name}/")
            if s.start in failed_states or (any_failed and kind == "test"
                                            and s.start != "null"
                                            and s.start in failed_states):
                pass  # covered by the first condition; kept for clarity
            if s.start in failed_states:
                results.record(**entry, status="skipped", duration_s=0.0)
                if s.saves:
                    failed_states.add(s.saves)
                continue

            t0 = time.monotonic()
            status, fk = "pass", None
            try:
                asyncio.run(session.ensure(s.nodes))
            except Exception as e:
                (art / "driver.log").write_text(f"[runner] session: {e}\n")
                status, fk = "fail", "environment"
            else:
                env = dict(base_env,
                           ASTRAL_TESTS_FACTS_OUT=str(art / "facts.json"))
                rc = _run_step(py, s.dir / "script.py", art / "driver.log",
                               env, s.timeout)
                if rc != 0:
                    status, fk = "fail", "driver"
                else:
                    session.merge_facts(art / "facts.json")
                    rc = _run_step(py, s.dir / "verify.py",
                                   art / "verify.log", env, s.timeout)
                    if rc != 0:
                        status, fk = "fail", "verify"
                if status == "fail" and session.dead_nodes():
                    fk = "environment"   # a dead daemon outranks any other blame

            results.record(**entry, status=status, failure_kind=fk,
                           duration_s=time.monotonic() - t0)
            if status != "pass":
                any_failed = True
                if s.saves:
                    failed_states.add(s.saves)
    finally:
        if args.keep or any_failed:
            print(f"run: session kept at {session.dir}", file=sys.stderr)
        else:
            session.teardown()
        if not (args.keep or any_failed):
            pass
        else:
            session.teardown() if False else None  # nodes keep running only with --keep

    code = results.finalize()
    print(f"run: results at {run_dir / 'results.json'}")
    return code
```

Wait — the teardown block above is convoluted. Use exactly this instead (final form):

```python
    finally:
        keep_procs = args.keep
        if not keep_procs:
            session.teardown()
        else:
            print(f"run: --keep, nodes left running under {session.dir}",
                  file=sys.stderr)
```

(Node logs and roots stay on disk under `results/<stamp>/session/` either way — `teardown()` stops processes, it never deletes files. `--keep` additionally leaves the daemons running for interactive poking.)

- [ ] **Step 3: Live-verify — the spine end-to-end**

Run: `./tests/run --lane node --only smoke`
Expected: exit 0; output names the results path. Then:
```bash
python3 - <<'EOF'
import json, pathlib
run = sorted(pathlib.Path("tests/results").iterdir())[-1]
doc = json.loads((run / "results.json").read_text())
assert doc["sandbox"] == "host" and doc["entries"][0] == doc["entries"][0]
e = doc["entries"][0]
assert (e["test"], e["kind"], e["status"]) == ("node/smoke", "test", "pass"), e
print("SPINE OK:", e)
EOF
```
Expected: `SPINE OK: {'test': 'node/smoke', ... 'status': 'pass', ...}`

- [ ] **Step 4: Commit**

```bash
git add tests/lib/runner.py tests/node/scenarios/smoke
git commit -m "feat(tests): runner orchestration + smoke scenario — spine closes"
```

---

### Task 8: bootstrap-user-software-key scenario

**Files:**
- Create: `tests/node/scenarios/bootstrap-user-software-key/{scenario.toml,script.py,verify.py,README.md}`

**Interfaces:**
- Consumes: session facts contract. Produces facts: `user_id` (66-hex), `user_token` (str) — adopt-node depends on exactly these names.

- [ ] **Step 1: Write the scenario**

`scenario.toml`:
```toml
start = "null"
saves = "one-node"
nodes = ["node1"]
drivers = ["script"]
timeout = 120
```

`script.py`:
```python
#!/usr/bin/env python3
"""Driver: create a software User key on node1 and activate its node contract.

Deterministic port of the node-setup playbook (netsim drove this via an AI
agent; here it is the exact op chain).
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        # A. software User key: entropy -> mnemonic -> seed -> private key
        entropy = await c.call_one("bip137sig.new_entropy", bits=256)
        mnemonic = (await c.call_with("bip137sig.mnemonic", entropy))[0]
        seed = (await c.call_with("bip137sig.seed", mnemonic))[0]
        privkey = (await c.call_with("bip137sig.derive_key", seed,
                                     path="m/44'/0'/0'/0/0"))[0]

        # B. persist the key (crypto indexes it as a signer) + derive identity
        await c.objects.store(privkey)
        user_pub = (await c.call_with("crypto.public_key", privkey))[0]
        user_id = str(user_pub)

        # C. build, sign, and activate the node contract
        contract = await c.call_one("user.new_node_contract", user=user_id)
        signed = (await c.call_with("auth.sign_contract", contract))[0]
        await c.user.accept_contract(signed)

        # D. mint an apphost token so later steps act AS the User
        access = await c.apphost.create_token(user_id)

    write_facts({"user_id": user_id, "user_token": access.token})
    print(f"driver: user {user_id[:16]}… bootstrapped, contract active")


asyncio.run(main())
```

`verify.py`:
```python
#!/usr/bin/env python3
"""Oracle: acting as the User works and an active contract exists.

Ported from netsim bootstrap-user-software-key/verify.sh: whoami must equal
the user id; user.info succeeding IS the proof of an active contract (it
rejects with code 2 when none is active).
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_id = doc["facts"]["user_id"]
    user_token = doc["facts"]["user_token"]
    assert user_id and user_token, "bootstrap facts missing"

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        who = await c.apphost.whoami()
        assert str(who) == user_id, f"whoami {who} != user {user_id}"
        info = await c.user.info()          # raises if no active contract
        assert str(info.user_id) == user_id, (
            f"contract issuer {info.user_id} != user {user_id}")
        assert str(info.node_id) == n1["identity"], (
            f"contract subject {info.node_id} != node {n1['identity']}")
    print("oracle: user identity active, contract issuer/subject correct")


asyncio.run(main())
```

`README.md`:
```markdown
# bootstrap-user-software-key

Start: null → saves: one-node. The driver replays the node-setup op chain
deterministically: bip137sig entropy→mnemonic→seed→key, store the key,
derive the User identity, build+sign+accept the node contract, mint a User
apphost token (published as session facts user_id / user_token). The oracle
connects WITH that token and asserts whoami == user and user.info shows the
contract issued by the user for this node.
```

- [ ] **Step 2: Live-verify**

Run: `./tests/run --lane node --only bootstrap-user-software-key`
Expected: exit 0; `driver.log` ends with `bootstrapped, contract active`; `verify.log` ends with `issuer/subject correct`. If `call_one`/`call_with` argument shapes mismatch an op (reject or ProtocolError), check the op's doc under `~/work/astralp2p/astral-docs/master/protocols/<module>/ops/` before changing code — and if an op is missing from astral-py with no `astral-query` fallback path, STOP and report (goal condition).

- [ ] **Step 3: Commit**

```bash
git add tests/node/scenarios/bootstrap-user-software-key
git commit -m "feat(tests): bootstrap-user-software-key — deterministic node-setup flow"
```

---

### Task 9: adopt-node scenario — the loopback seam

**Files:**
- Create: `tests/node/scenarios/adopt-node/{scenario.toml,script.py,verify.py,README.md}`

**Interfaces:**
- Consumes: facts `user_id`, `user_token` from bootstrap; both nodes' `identity`/`tcp_port` from session.json.

- [ ] **Step 1: Write the scenario**

`scenario.toml`:
```toml
start = "one-node"
saves = "two-nodes"
nodes = ["node1", "node2"]
drivers = ["script"]
timeout = 180
```

`script.py`:
```python
#!/usr/bin/env python3
"""Driver: link node2 over loopback with explicit endpoints, adopt it, alias both.

netsim's version rode LAN discovery (nearby); loopback has none, so this is
the playbook's explicit-endpoint path: add_endpoint both ways, new_link with
a direct endpoint, then user.adopt as the User.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]
    ep1 = f"tcp:127.0.0.1:{n1['tcp_port']}"
    ep2 = f"tcp:127.0.0.1:{n2['tcp_port']}"

    # each node learns the other's endpoint (link-back after adoption)
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.call("nodes.add_endpoint", id=n2["identity"], endpoint=ep2)
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        await c.call("nodes.add_endpoint", id=n1["identity"], endpoint=ep1)

    # as the User on node1: explicit link, then adopt
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.call("nodes.new_link", target=n2["identity"], endpoint=ep2,
                     strategies="basic")
        await c.user.adopt(n2["identity"])
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")

    print("driver: node2 linked and adopted, aliases set on both nodes")


asyncio.run(main())
```

`verify.py`:
```python
#!/usr/bin/env python3
"""Oracle: symmetric swarm — port of netsim adopt-node/verify.py assertions.

Both-ends check: same User issued both contracts; each node's linked sibling
is the other; node2 holds a live link back to node1.
"""
import asyncio

import astral

from lib.sessionio import load


def linked_sibling(members):
    for m in members:
        if m.linked and m.identity is not None:
            return str(m.identity)
    return None


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_id = doc["facts"]["user_id"]
    user_token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        i1 = await c.user.info()
        assert str(i1.user_id) == user_id, "node1 contract not issued by User"
        sib1 = linked_sibling(await c.user.swarm_status())
        assert sib1 == n2["identity"], (
            f"node1 linked sibling {sib1} != node2 {n2['identity']}")

    async with await astral.connect(n2["endpoint"]) as c:      # anonymous
        i2 = await c.user.info()
        assert str(i2.user_id) == user_id, "node2 adopted under a different User"
        sib2 = linked_sibling(await c.user.swarm_status())
        assert sib2 == n1["identity"], (
            f"node2 linked sibling {sib2} != node1 {n1['identity']} "
            "(symmetric-roster regression)")
        links = await c.call("nodes.links")
        remotes = {str(l.remote_identity) for l in links}
        assert n1["identity"] in remotes, f"no link back to node1 in {remotes}"

    print("oracle: symmetric roster, same issuer, linkback present")


asyncio.run(main())
```

`README.md`:
```markdown
# adopt-node

Start: one-node → saves: two-nodes. The driver registers explicit loopback
endpoints on both nodes (no LAN discovery exists here), opens a direct tcp
link (strategies=basic), adopts node2 into the User's swarm, and registers
node1/node2 dir aliases on both. The oracle is the netsim adopt verifier
ported: same-issuer contracts on both nodes, symmetric linked-sibling
roster, and a live link from node2 back to node1.
```

- [ ] **Step 2: Live-verify — chain and seam**

Run: `./tests/run --lane node --only adopt-node`
Expected: exit 0; results.json has TWO entries — `node/bootstrap-user-software-key` with `"kind": "fixture"` and `node/adopt-node` with `"kind": "test"`, both `pass`. This is exit criterion 2 and the loopback seam proof in one run. If `user.adopt` rejects: code 2 = initiator has no active contract (bootstrap fixture broken), code 3 = caller is not the User (token mixup) — read `driver.log` and the node logs under `results/<stamp>/session/`.

- [ ] **Step 3: Full lane**

Run: `./tests/run --lane node`
Expected: exit 0; three entries (bootstrap, smoke, adopt-node — depth order), all `"kind": "test"`, all `pass`.

- [ ] **Step 4: Commit**

```bash
git add tests/node/scenarios/adopt-node
git commit -m "feat(tests): adopt-node — explicit-endpoint loopback adoption + symmetric-roster oracle"
```

---

### Task 10: Migrate netsim/ → tests/net/ + tests/stages/

**Files:**
- Move (`git mv`, history-preserving): `netsim/tasks` → `tests/net/tasks` (includes `_lib/` and the `_lib/astral-py` **git submodule**), `netsim/link.sh` → `tests/net/link.sh`, `netsim/README.md` → `tests/net/README.md`; `netsim/scenarios/{lab,bootstrap-user-software-key,adopt-node}` → `tests/stages/`; every remaining `netsim/scenarios/*` → `tests/net/scenarios/`.
- Modify: path references to the old `netsim/` location in the moved README, `link.sh` (if any non-relative path), and repo docs (`docs/running-as-a-service.md` is a known referrer).

**Interfaces:**
- Consumes: nothing from earlier tasks (pure relocation; independent of the runner).
- Produces: goal criterion 6 — the settled layout, with the existing netsim workflow (`link.sh` registration, `netsim story/task` invocations) working from the new home. Task internals are NOT rewritten.

- [ ] **Step 1: Preflight inventory**

Run: `ls netsim/ netsim/scenarios/ && git config -f .gitmodules --get-regexp path`
Expected: `netsim/` holds `tasks/ scenarios/ link.sh README.md` (move anything extra with the same pattern); submodule path `netsim/tasks/_lib/astral-py` listed.

- [ ] **Step 2: Move**

```bash
mkdir -p tests/net tests/stages
git mv netsim/tasks tests/net/tasks
git mv netsim/link.sh tests/net/link.sh
git mv netsim/README.md tests/net/README.md
for s in lab bootstrap-user-software-key adopt-node; do
  git mv "netsim/scenarios/$s" "tests/stages/$s"
done
git mv netsim/scenarios tests/net/scenarios
rmdir netsim
git status --short | head -20
```
Expected: only renames (`R`) plus `.gitmodules` modified; `netsim/` gone.

- [ ] **Step 3: Submodule integrity**

Run: `git config -f .gitmodules --get-regexp path && git submodule status | head -3`
Expected: path now `tests/net/tasks/_lib/astral-py`; status resolves (git mv rewrites .gitmodules since 1.8.5). If the path did not update: edit `.gitmodules` to the new path and run `git submodule sync`.

- [ ] **Step 4: Fix path references**

Run: `grep -rn "netsim/" tests/net tests/stages docs README.md AGENTS.md 2>/dev/null | grep -v "_lib/astral-py" | head -40`
For each hit: rewrite `netsim/tasks/...` → `tests/net/tasks/...`, `netsim/scenarios/<stage-builder>` → `tests/stages/<...>`, other `netsim/scenarios/...` → `tests/net/scenarios/...`, story invocation examples accordingly. `link.sh` resolves its tasks dir relative to `$0` — verify it still finds `tests/net/tasks/*` (fix only if it hardcodes the old path). Do NOT edit task internals (`run.sh`/`verify.py` use `$0`-relative `_lib` paths that move intact).

- [ ] **Step 5: Criterion-6 verify — workflow works from the new home**

```bash
test ! -d netsim && echo "NETSIM-GONE"
./tests/net/link.sh
netsim tasks | head -20
```
Expected: `NETSIM-GONE`; `link.sh` completes; the user tasks (install-astrald, adopt-node, bootstrap-user-software-key, …) listed by `netsim tasks` from their new location.

- [ ] **Step 6: Coordination + commit**

Log the coordination line in both Outline task documents (this task's Log and the "netsim: re-run scenarios…" task's Log) per the goal condition, then:

```bash
git add -A
git commit -m "refactor(tests): migrate netsim/ into tests/net/ + tests/stages/ (structural)"
```

---

### Task 11: Exit-criteria run, README, PR

**Files:**
- Create: `tests/README.md`
- No code changes — this task executes and documents the six goal criteria.

- [ ] **Step 1: Criterion 1 — full lane green**

Run: `./tests/run --lane node && echo "EXIT: $?"`
Expected: `EXIT: 0`, three pass entries. Save the tail of the output.

- [ ] **Step 2: Criterion 2 — selection builds its fixture**

Run: `./tests/run --lane node --only adopt-node && echo "EXIT: $?"`
Expected: `EXIT: 0`; results.json shows bootstrap as `fixture`, adopt-node as `test`.

- [ ] **Step 3: Criterion 3 — sabotaged oracle → fail/verify**

```bash
printf '\nraise SystemExit("sabotaged")\n' >> tests/node/scenarios/smoke/verify.py
./tests/run --lane node --only smoke; echo "EXIT: $?"
git checkout -- tests/node/scenarios/smoke/verify.py
```
Expected: `EXIT: 1`; last events.jsonl line has `"status": "fail", "failure_kind": "verify"`.

- [ ] **Step 4: Criterion 4 — killed node → fail/environment**

```bash
( sleep 6; pkill -f "session/node1" ) & ./tests/run --lane node --only adopt-node; echo "EXIT: $?"
```
Expected: `EXIT: 1`; the failing entry carries `"failure_kind": "environment"` (the dead-daemon override outranks driver/verify blame). Adjust the sleep if the kill lands before node1 exists — it must land mid-scenario.

- [ ] **Step 5: Criterion 5 — schema validity**

```bash
python3 - <<'EOF'
import json, pathlib, sys
sys.path.insert(0, "tests")
from lib.results import STATUSES, FAILURE_KINDS, KINDS
for run in sorted(pathlib.Path("tests/results").iterdir()):
    p = run / "results.json"
    if not p.exists():
        continue
    doc = json.loads(p.read_text())
    assert {"astrald_ref", "host", "sandbox", "lanes", "hermetic", "started",
            "wall_time_s", "entries"} <= set(doc), run
    for e in doc["entries"]:
        assert e["status"] in STATUSES and e["kind"] in KINDS, e
        assert ("failure_kind" in e) == (e["status"] == "fail"), e
        if "failure_kind" in e:
            assert e["failure_kind"] in FAILURE_KINDS, e
print("ALL RESULTS SCHEMA-VALID")
EOF
```
Expected: `ALL RESULTS SCHEMA-VALID`

- [ ] **Step 6: Criterion 6 — migration landed**

```bash
test ! -d netsim && echo "NETSIM-GONE"
./tests/net/link.sh && netsim tasks | head -8
```
Expected: `NETSIM-GONE`; tasks registered and listed from `tests/net/`. Quote alongside criteria 1–5.

- [ ] **Step 7: Write tests/README.md**

```markdown
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
```

- [ ] **Step 8: Quote evidence + commit + push**

Append the six criteria outputs (trimmed) to the Outline task document's `## Log` with date 2026-07-29, then:

```bash
git add tests/README.md
git commit -m "docs(tests): M1 README + exit-criteria evidence run"
git push -u origin intern0/dev/tests-m1-skeleton
```

Expected: push prints the Forgejo compare URL — record it in the task document Header (`PR: pushed, open from <url>`) and send the telegram-notify one-liner per the goal condition. Opening/merging the PR is the operator's step.

---

## Self-review (run after writing, fix inline)

1. **Spec coverage:** goal items — runner ✓ (T1, T7), localnode ✓ (T5), session ✓ (T6), results ✓ (T2), three scenarios ✓ (T7-T9), venv bootstrap ✓ (T1), structural migration ✓ (T10), criteria run ✓ (T11). Suites/VM/agent + net-lane *execution*: correctly absent (M2+); the netsim *content* migration is in per the expanded goal.
2. **Placeholder scan:** no TBDs; every step carries code or an exact command with expected output.
3. **Type consistency:** `ports_for(base, index) -> NodePorts(apphost, tcp, ether)` used identically in T4/T5/T6; facts keys `user_id`/`user_token` written in T8 and read in T9; `RunResults.record` signature in T2 matches every call in T7; `sessionio.load()` shape `{"nodes": {name: {endpoint, token, identity, root, tcp_port}}, "facts": {}}` written in T6 and consumed in T7-T9.
