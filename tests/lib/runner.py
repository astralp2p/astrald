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
    try:
        for s, kind in plan:
            art = run_dir / "node" / s.name
            art.mkdir(parents=True, exist_ok=True)
            entry = dict(test=f"node/{s.name}", kind=kind, lane="node",
                         driver="script", artifacts=f"node/{s.name}/")
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
                if session.dead_nodes():
                    status, fk = "fail", "environment"   # a dead daemon voids
                    # any verdict — even a nominal pass over a dead world

            results.record(**entry, status=status, failure_kind=fk,
                           duration_s=time.monotonic() - t0)
            if status != "pass" and s.saves:
                failed_states.add(s.saves)
    finally:
        if args.keep:
            print(f"run: --keep, nodes left running under {session.dir}",
                  file=sys.stderr)
        else:
            session.teardown()

    code = results.finalize()
    print(f"run: results at {run_dir / 'results.json'}")
    return code
