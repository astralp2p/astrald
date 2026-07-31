"""Orchestration: selection, suites, execution through one executor, results."""
import os
import platform
import subprocess
import sys
import time
import tomllib
from pathlib import Path

from astral.client import ENDPOINT_VARS, TOKEN_VARS

from lib import manifest, suites
from lib.build import ensure_binary, git_ref
from lib.executors import ExecutorError
from lib.executors.attach import AttachExecutor
from lib.executors.local import LocalExecutor
from lib.results import RunHeader, RunResults, fresh_run_dir

TESTS = Path(__file__).resolve().parent.parent
REPO = TESTS.parent
DEFAULT_SUITE = "main.suite"


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


def driver_env(session_json: Path, hermetic: bool) -> dict:
    """The environment drivers and oracles run in.

    why: astral-py resolves an endpoint and a token from the ambient
    environment whenever a call passes none, so a developer's own daemon
    token answers for a test's anonymous connect and the test node refuses
    it. A hermetic run resolves nothing from outside session.json; --target
    attach is the one mode where the ambient resolution is the point.
    """
    env = dict(os.environ,
               PYTHONPATH=os.environ.get("ASTRAL_TESTS_PYPATH", str(TESTS)),
               ASTRAL_TESTS_SESSION=str(session_json))
    if hermetic:
        for var in ENDPOINT_VARS + TOKEN_VARS:
            env.pop(var, None)
    return env


def plan_for(tests: dict, selection: list) -> list:
    """[(Test, kind)] for what the CLI selected: a suite runs exactly as
    listed, a bare test selection gets the fixture prefix its starts need."""
    suites_dir = TESTS / "suites"
    if not selection:
        selection = [DEFAULT_SUITE]
    named = [n for n in selection if suites.find(suites_dir, n)]
    if named and len(selection) > 1:
        raise ValueError(f"a suite runs alone: {named[0]!r} cannot be mixed "
                         "with other selections")
    if named:
        listed = suites.load(suites.find(suites_dir, named[0]))
        manifest.validate_order(tests, listed)
        return [(tests[n], "test") for n in listed]
    return manifest.resolve(tests, selection)


def _reject_unbuilt(args, plan: list) -> str | None:
    """What the runner cannot do yet, and why — never a silent wrong verdict."""
    if args.driver == "agent":
        # fixme: the agent driver needs an AI operator with the astral-agent
        # skill. Env netsim would supply the lab-baked one and env node the
        # disposable VM sandbox; both are VMs, and netsim does not run on this
        # host. prompt.md ships with seven tests, ready for it.
        return ("--driver agent needs an operator VM: netsim does not run on "
                "this host (NETSIM_STAGES_DIR -> root-owned /mnt/netsim)")
    if args.target == "attach":
        # why: attach runs against the operator's own daemon. Building a
        # fixture prefix there means running bootstrap's flow at it — which
        # stores a private key object and attempts a contract on a live node.
        # The design says attach CHECKS a start state; it does not build one.
        built = [t.name for t, kind in plan if kind == "fixture"]
        if built:
            return (f"--target attach would have to build {built[0]!r} on the "
                    "attached daemon. Attach checks a start state, it never "
                    "builds one — select a test whose start the daemon "
                    "already stands at.")
    if args.target.startswith("stage:"):
        return (f"--target {args.target} needs the netsim executor, which is "
                "not wired to the runner (see lib/executors/netsim.py)")
    other = sorted({t.env for t, _ in plan} - {"node"})
    if other:
        # fixme: lib/executors/netsim.py exists but is not reachable from here.
        # Its session.json carries no working endpoints yet — a guest apphost
        # is netns-local once a node is NAT'd, and the host-side tunnel that
        # reaches it cannot be designed without a live netsim. netsim does not
        # run on this host (NETSIM_STAGES_DIR -> root-owned /mnt/netsim).
        return (f"env {other[0]} is not wired to the runner: the netsim "
                "executor's session tunnel is unbuilt (see "
                "lib/executors/netsim.py)")
    undeclared = [t.name for t, _ in plan if args.driver not in t.drivers]
    if undeclared:
        return f"{undeclared[0]} declares no {args.driver} driver"
    return None


def _executor(args, dir, binary, port_base):
    """Where the machines come from — orthogonal to env and driver."""
    if args.target == "attach":
        return AttachExecutor(dir)
    return LocalExecutor(dir, binary, port_base)


def main(args) -> int:
    cfg = tomllib.loads((TESTS / "config.toml").read_text())

    try:
        tests = manifest.load_all(TESTS / "e2e")
        plan = plan_for(tests, args.selection)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    unbuilt = _reject_unbuilt(args, plan)
    if unbuilt:
        print(f"run: {unbuilt}", file=sys.stderr)
        return 2

    if args.target == "attach":
        binary, ref = None, git_ref(REPO)     # the daemon is already running
    else:
        binary, ref = ensure_binary(REPO)
    run_dir = fresh_run_dir(TESTS / "results")
    results = RunResults(run_dir, RunHeader(
        astrald_ref=ref,
        astral_py_ref=git_ref(Path(os.path.expanduser(
            cfg["astral_py"]["path"]))),
        host=platform.node(), sandbox="host",
        hermetic=args.target == "fresh"))

    ex = _executor(args, run_dir / "session", binary, cfg["ports"]["base"])
    py = sys.executable
    base_env = driver_env(ex.session_json_path, results.header["hermetic"])

    mutators = [t.name for t, _ in plan if t.mutates]
    if args.target != "fresh" and mutators:
        print(f"run: --target {args.target}: {', '.join(mutators)} really "
              "mutates this world — it is not a throwaway", file=sys.stderr)

    broken_states = set()
    try:
        for t, kind in plan:
            art = run_dir / t.name
            art.mkdir(parents=True, exist_ok=True)
            entry = dict(test=t.name, kind=kind, env=t.env,
                         driver=args.driver, artifacts=f"{t.name}/")
            dead = manifest.blocked_by(tests, t.start, broken_states)
            if dead:
                (art / "driver.log").write_text(
                    f"[runner] skipped: state {dead!r} was never built\n")
                results.record(**entry, status="skipped", duration_s=0.0)
                if t.saves:
                    broken_states.add(t.saves)
                continue

            t0 = time.monotonic()
            status, fk = "pass", None
            try:
                ex.prepare(t)
            except ExecutorError as e:
                (art / "driver.log").write_text(f"[runner] {e}\n")
                status, fk = "fail", "environment"
            else:
                env = dict(base_env,
                           ASTRAL_TESTS_FACTS_OUT=str(art / "facts.json"))
                rc = _run_step(py, t.dir / "script.py", art / "driver.log",
                               env, t.timeout)
                if rc != 0:
                    status, fk = "fail", "driver"
                else:
                    ex.merge_facts(art / "facts.json")
                    rc = _run_step(py, t.dir / "verify.py",
                                   art / "verify.log", env, t.timeout)
                    if rc != 0:
                        status, fk = "fail", "verify"
                if ex.broken():
                    status, fk = "fail", "environment"   # a dead daemon voids
                    # any verdict — even a nominal pass over a dead world

            results.record(**entry, status=status, failure_kind=fk,
                           duration_s=time.monotonic() - t0)
            if status == "pass":
                if t.saves:
                    ex.save(t.saves)
            elif t.saves:
                broken_states.add(t.saves)
    finally:
        ex.teardown(args.keep)
        if args.keep:
            print(f"run: --keep, world left up under "
                  f"{ex.session_json_path.parent}", file=sys.stderr)

    code = results.finalize()
    print(f"run: results at {run_dir / 'results.json'}")
    return code
