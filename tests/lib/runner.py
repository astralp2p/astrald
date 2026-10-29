"""Orchestration: selection, suites, execution through one executor, results.

The runner owns run state: the world record, every test's facts, and the
session view each driver and oracle reads. An executor owns machines only.
"""
import json
import os
import platform
import subprocess
import sys
import time
import tomllib
import traceback
from pathlib import Path

from astral.client import ENDPOINT_VARS, TOKEN_VARS

from lib import manifest, report, suites
from lib import plan as planner
from lib.build import ensure_binary, git_ref
from lib.executors import ExecutorError
from lib.executors.attach import AttachExecutor
from lib.executors.local import LocalExecutor
from lib.executors.netsim import NetsimExecutor
from lib.nodeconfig import PortsBusy, lease_ports
from lib.record import WorldRecord
from lib.results import RunHeader, RunResults, fresh_run_dir

TESTS = Path(__file__).resolve().parent.parent
REPO = TESTS.parent
RESULTS = TESTS / "results"
DEFAULT_SUITE = "main.suite"
WORLD = "w0"          # one world per run, until the planner splits a run


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


def _perform(ex, t, driver: str, art: Path, view) -> tuple:
    """(status, failure_kind, phase) of one test: prepare, driver, the
    restart the manifest names, then the oracle. `phase` is the step the
    test stopped at.

    `view(phase)` writes the test's session view for the phase about to run
    and returns the environment that phase runs in. The oracle gets a fresh
    one: a restart changes the nodes, and the oracle also reads what its own
    driver wrote.

    why one try around every step: an operator's model rewrite, payload push
    or ssh raises from inside the driver step, and a try around prepare alone
    let it leave the loop with no verdict recorded and no report written. An
    ExecutorError is the world breaking, so it is `environment` at its step.
    """
    phase = "prepare"
    try:
        ex.prepare(t)
        env = view("driver")
        phase = "driver"
        if driver == "agent":
            rc = ex.run_agent(t, art / "driver.log", art / "facts.json",
                              t.agent_timeout)
        else:
            rc = _run_step(sys.executable, t.dir / "script.py",
                           art / "driver.log", env, t.timeout)
        if rc != 0:
            return "fail", "driver", phase
        if t.restart:
            phase = "restart"
            ex.restart(t.restart)
        phase = "verify"
        rc = _run_step(sys.executable, t.dir / "verify.py",
                       art / "verify.log", view("verify"), t.timeout)
    except ExecutorError as e:
        with (art / "driver.log").open("a") as f:
            f.write(f"[runner] {e}\n")
        return "fail", "environment", phase
    except BaseException as e:
        e.phase = phase       # note: the abort path records the step it hit
        raise
    return ("pass", None, phase) if rc == 0 else ("fail", "verify", phase)


def _entry(t, kind: str, env: str, driver: str) -> dict:
    """The fields every record of a test carries, whatever its status.

    why env is the run's: a record states the env the test RAN in, not the
    one its manifest asks for as a minimum. A node test lifted into VMs — by
    a netsim test ahead of it, or by the agent driver — really ran there, and
    a record that said otherwise would hide the one property the design is
    built on.
    """
    return dict(test=t.name, kind=kind, env=env, driver=driver, world=WORLD,
                artifacts=f"{t.name}/")


def _skip(results, entry: dict, reason: str, phase: str = "prepare") -> None:
    """Record a test that never finished, with its reason in its driver.log.

    note: a skip never joins the world record's history — nothing ran.
    """
    art = results.dir / entry["test"]
    art.mkdir(parents=True, exist_ok=True)
    with (art / "driver.log").open("a") as f:
        f.write(f"[runner] skipped: {reason}\n")
    results.record(**entry, status="skipped", duration_s=0.0,
                   phase=phase, reason=reason)


def _abort_reason(e: BaseException) -> str:
    """What ended a run early, in one line."""
    if isinstance(e, KeyboardInterrupt):
        return "interrupted"
    first = (str(e).splitlines() or [""])[0]
    return f"{type(e).__name__}: {first}" if first else type(e).__name__


def _teardown(ex, keep: bool) -> BaseException | None:
    """Tear the world down; the error that stopped it, if any.

    why caught: the report is written after teardown, and a teardown that
    raised used to leave none — or a PASS report above a traceback.
    """
    try:
        ex.teardown(keep)
    except (KeyboardInterrupt, Exception) as e:
        if not isinstance(e, KeyboardInterrupt):
            traceback.print_exc()
        return e
    return None


def _finish(results, ex) -> int:
    """Write results.json and report.md and print the verdict: the run's
    exit code."""
    # why: a VM verdict is about the code under test only on the machines
    # that run it, so the header names them
    if ex.pushed:
        results.header["pushed"] = ",".join(ex.pushed)
    code = results.finalize()
    print(f"\n{report.summary_line(results.doc)}")
    print(f"run: report at {results.dir / 'report.md'}")
    return code


def write_view(path: Path, run: str, nodes: dict, facts: dict) -> None:
    """A test's session view: the one file its driver and oracle read.

    why the runner writes it and no executor does: three executors wrote
    three schemas, and only one carried every node field. Each test gets
    its own, in a real directory under results/<stamp>/, so the view's
    path names the run it belongs to.
    """
    doc = {"run": run, "nodes": nodes, "facts": facts}
    path.write_text(json.dumps(doc, indent=2) + "\n")


def _note_nodes(record: WorldRecord, ex) -> None:
    """Write the world's nodes into its record, as the executor reports them.

    why here and not per view: a run whose first prepare failed, the usual
    reason for --keep, wrote no view, and its record named no nodes at all.
    A world too broken to report its nodes keeps whatever the record held.
    """
    try:
        record.nodes = ex.nodes()
    # why every exception: this runs in the finally that writes the report,
    # and whatever escaped it would cost the run its report
    except Exception as e:                    # noqa: BLE001
        print(f"run: the world reported no nodes: {e}", file=sys.stderr)
    record.write()


def own_facts(path: Path) -> dict:
    """What a driver wrote with write_facts, or nothing when it wrote none."""
    return json.loads(path.read_text()) if path.exists() else {}


def driver_env(session_json: Path, target: str) -> dict:
    """The environment drivers and oracles run in.

    why: astral-py resolves an endpoint and a token from the ambient
    environment whenever a call passes none, so a developer's own daemon
    token answers for a test's anonymous connect and the test node refuses
    it. Every target but attach resolves nothing from outside session.json;
    attach is the one mode where the ambient resolution is the point.

    note: this keys off the target, not the header's `hermetic`. A pinned
    stage is not hermetic, yet its nodes are the harness's, not the
    developer's.
    """
    env = dict(os.environ,
               PYTHONPATH=os.environ.get("ASTRAL_TESTS_PYPATH", str(TESTS)),
               ASTRAL_TESTS_SESSION=str(session_json))
    if target != "attach":
        for var in ENDPOINT_VARS + TOKEN_VARS:
            env.pop(var, None)
    return env


def selected_suite(selection: list) -> list | None:
    """The tests the suite the CLI named lists, or None for a bare test
    selection. A suite runs alone."""
    suites_dir = TESTS / "suites"
    named = [n for n in selection if suites.find(suites_dir, n)]
    if named and len(selection) > 1:
        raise ValueError(f"a suite runs alone: {named[0]!r} cannot be mixed "
                         "with other selections")
    return suites.load(suites.find(suites_dir, named[0])) if named else None


def plan_for(tests: dict, selection: list) -> list:
    """[(Test, kind)] for what the CLI selected.

    A suite runs exactly as listed, once its order walks. A bare test
    selection gets the prereq prefix its starts need, mutators last and
    observers before producers at equal depth; it is refused when it holds
    two mutators, and otherwise held to the same walk a suite is.
    """
    listed = selected_suite(selection or [DEFAULT_SUITE])
    if listed:
        manifest.validate_order(tests, listed)
        return [(tests[n], "test") for n in listed]
    plan = manifest.resolve(tests, selection)
    # why: one live world serves the whole selection, and each mutator
    # invalidates the state the other starts from, so no order runs both
    mutators = [t.name for t, _ in plan if t.mutates]
    if len(mutators) > 1:
        raise ValueError(f"{mutators[0]} and {mutators[1]} both mutate the "
                         "world, so no order runs both: select them in "
                         "separate runs")
    # why: a bare selection is held to the walk a suite is held to, so a
    # destroyer can never run ahead of a test that needs what it destroys
    manifest.validate_order(tests, [t.name for t, _ in plan])
    return plan


def print_plan(tests: dict, args) -> int:
    """--plan: the worlds the planner makes of the selection, printed, with
    nothing built, leased or started."""
    if args.target.startswith("stage:"):
        print("run: --plan plans fresh and attach runs, not a pinned stage",
              file=sys.stderr)
        return 2
    selection = args.selection or [DEFAULT_SUITE]
    try:
        listed = selected_suite(selection)
        worlds = planner.plan(tests, listed or selection, listed or (),
                              isolate=args.isolate, driver=args.driver,
                              target=args.target,
                              prereqs_agent=args.prereqs == "agent")
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    print(planner.render(worlds), end="")
    return 0


def _reject_unbuilt(args, plan: list) -> str | None:
    """What the runner cannot do yet, and why — never a silent wrong verdict."""
    if args.driver == "agent" and args.target == "attach":
        # why: the operator lives in the simulated world. Attach has no world
        # of its own — it borrows a daemon — so there is nobody to drive.
        return "--driver agent has no operator under --target attach"
    if args.target == "attach":
        # why: attach runs against the operator's own daemon. Building a
        # prereq prefix there means running bootstrap's flow at it — which
        # stores a private key object and attempts a contract on a live node.
        # The design says attach CHECKS a start state; it does not build one.
        built = [t.name for t, kind in plan if kind == "prereq"]
        if built:
            return (f"--target attach would have to build {built[0]!r} on the "
                    "attached daemon. Attach checks a start state, it never "
                    "builds one — select a test whose start the daemon "
                    "already stands at.")
    if args.target == "attach" and run_env(plan, args.driver, args.target) == "netsim":
        return "--target attach has no simulation to attach to"
    restarting = [t.name for t, kind in plan if kind == "test" and t.restart]
    if restarting and (args.target != "fresh"
                       or run_env(plan, args.driver, args.target) != "node"):
        # why refused up front: the driver changes the node before the
        # restart, and only env node's fresh world can restart one.
        return (f"{restarting[0]} restarts a node, which only env node's "
                "fresh world can do")
    undeclared = [t.name for t, _ in plan if args.driver not in t.drivers]
    if undeclared:
        return f"{undeclared[0]} declares no {args.driver} driver"
    return None


def run_env(plan: list, driver: str = "script",
            target: str = "fresh") -> str:
    """The env this run executes in: netsim as soon as any selected test needs it.

    why: a netsim test's prereq prefix is env-node tests, and they must build
    their states in the SAME world the netsim test will run in. Running them
    on loopback would build a state the VMs never see. This is the env lift
    the design promises — the same files, driven against VMs instead of
    processes, with nothing in them aware of the difference.

    The agent driver lifts a run the same way. An AI operator has to live
    somewhere and reach astrald as an app would, and the lab bakes exactly one
    such machine — so `--driver agent` runs in VMs whatever the tests declare.
    A manifest's env is the cheapest world that can falsify the test, not a
    ceiling.
    """
    # A pinned stage is a simulation by definition, whatever the selected
    # tests declare — so a node test run against one really did run in VMs.
    if driver == "agent" or target.startswith("stage:"):
        return "netsim"
    selected = [t for t, kind in plan if kind == "test"]
    return "netsim" if any(t.env == "netsim" for t in selected) else "node"


def _executor(args, plan, dir, binary, ports, ref):
    """Where the machines come from — orthogonal to env and driver."""
    if args.target == "attach":
        return AttachExecutor()
    if args.target.startswith("stage:"):
        ex = NetsimExecutor(binary, ref)
        ex.pinned = args.target.split(":", 1)[1]
        return ex
    if run_env(plan, args.driver, args.target) == "netsim":
        return NetsimExecutor(binary, ref)
    return LocalExecutor(dir, binary, lease_ports(**ports), keep=args.keep)


def main(args) -> int:
    cfg = tomllib.loads((TESTS / "config.toml").read_text())

    try:
        tests = manifest.load_all(TESTS / "e2e")
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    if getattr(args, "plan", False):
        return print_plan(tests, args)
    if getattr(args, "isolate", False) or getattr(args, "prereqs",
                                                   "script") != "script":
        # why refused rather than ignored: a run that silently shared a world
        # would read as an isolated verdict it never was
        print("run: --isolate and --prereqs take effect with --plan only, "
              "until runs follow the plan", file=sys.stderr)
        return 2
    try:
        plan = plan_for(tests, args.selection)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    if args.target.startswith("stage:"):
        # why: naming a stage asserts the world already stands where the
        # selection starts, so building its prefix would rebuild what the
        # operator just said is there. Attach makes the same assertion and
        # refuses instead; a stage is disposable, so here the prefix is
        # simply not run.
        plan = [(t, kind) for t, kind in plan if kind == "test"]
        if not plan:
            print("run: nothing to run against a pinned stage", file=sys.stderr)
            return 2

    unbuilt = _reject_unbuilt(args, plan)
    if unbuilt:
        print(f"run: {unbuilt}", file=sys.stderr)
        return 2

    if args.target == "attach":
        binary, ref = None, git_ref(REPO)     # the daemon is already running
    else:
        binary, ref = ensure_binary(REPO)
    run_dir = fresh_run_dir(RESULTS)
    results = RunResults(run_dir, RunHeader(
        astrald_ref=ref,
        astral_py_ref=git_ref(Path(os.path.expanduser(
            cfg["astral_py"]["path"]))),
        host=platform.node(), sandbox="host",
        hermetic=args.target == "fresh",
        selection=" ".join(args.selection) or DEFAULT_SUITE,
        target=args.target))

    env_of_run = run_env(plan, args.driver, args.target)
    try:
        ex = _executor(args, plan, run_dir / "session", binary,
                       cfg["ports"], ref)
    except PortsBusy as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    if args.driver == "agent":
        # why named rather than assumed: the profile IS the operator under
        # test, so the run records which one drove it. A pass rate that cannot
        # name its agent is not attributable to anything.
        agent_cfg = cfg.get("agent", {})
        name = agent_cfg.get("use", "qwen")
        profile = agent_cfg.get(name)
        if not isinstance(profile, dict):
            print(f"run: config.toml selects agent profile {name!r}, but has "
                  f"no [agent.{name}] section", file=sys.stderr)
            return 2
        ex.use_operator(profile)
        ex.want_model(profile.get("model", ""), profile.get("base_url", ""))
        results.header["agent_profile"] = name
        results.header["agent_model"] = profile.get("model", "") or "(lab default)"
    record = WorldRecord(run_dir, WORLD)

    mutators = [t.name for t, _ in plan if t.mutates]
    if args.target != "fresh" and mutators:
        print(f"run: --target {args.target}: {', '.join(mutators)} really "
              "mutates this world — it is not a throwaway", file=sys.stderr)

    broken_states = set()
    stop = None               # what ended the loop early, if anything
    try:
        for t, kind in plan:
            art = run_dir / t.name
            art.mkdir(parents=True, exist_ok=True)
            entry = _entry(t, kind, env_of_run, args.driver)
            dead = manifest.blocked_by(tests, t.start, broken_states)
            if dead:
                _skip(results, entry, f"state {dead} was never built")
                if t.saves:
                    broken_states.add(t.saves)
                continue

            t0 = time.monotonic()
            lineage = manifest.lineage(tests, t.start)
            env = dict(driver_env(art / "session.json", args.target),
                       ASTRAL_TESTS_FACTS_OUT=str(art / "facts.json"))

            driven = {}

            def view(phase: str) -> dict:
                # why lineage only: a test sees the facts of what its start
                # state was built from, never a sibling's. Its oracle also
                # sees its own driver's, read once before the oracle runs,
                # and those are what join the record on a pass.
                facts = record.facts_of(lineage)
                if phase == "verify":
                    driven.update(own_facts(art / "facts.json"))
                    facts |= driven
                write_view(art / "session.json", record.run, ex.nodes(),
                           facts)
                return env

            status, fk, phase = _perform(ex, t, args.driver, art, view)
            if fk != "environment" and ex.broken():
                # why: a dead daemon voids any verdict, even a nominal pass
                status, fk, phase = "fail", "environment", "alive"

            results.record(**entry, status=status, failure_kind=fk,
                           phase=phase, duration_s=time.monotonic() - t0)
            record.add(t.name, kind, status,
                       driven if status == "pass" else {})
            if status == "pass":
                if t.saves:
                    ex.save(t.saves)
            elif t.saves:
                broken_states.add(t.saves)
    # why Ctrl-C and every exception: a VM run lasts tens of minutes, and a
    # run that died without a report left only events.jsonl. Whatever ends
    # the loop, the tests it reached keep their verdicts and the rest say why.
    except BaseException as e:
        if isinstance(e, Exception):
            traceback.print_exc()
        stop = e
    finally:
        # why before teardown: a torn-down world reports no nodes, and a
        # kept one is read back from this record
        _note_nodes(record, ex)
        torn = _teardown(ex, args.keep)
        if args.keep:
            roots = run_dir / "session"
            print(f"run: --keep, world left up; its nodes are in "
                  f"{record.path}"
                  + (f", node roots and logs under {roots}"
                     if roots.exists() else ""), file=sys.stderr)
        aborted = []
        # note: the loop records each planned test once, in plan order, so
        # the tests past the last record never finished
        unrun = plan[len(results.entries):]
        if stop is not None or unrun:
            reason = (_abort_reason(stop) if stop is not None
                      else "the run stopped before its plan ended")
            aborted.append(reason)
            # why: the first test never recorded is the one the stop hit,
            # so it carries the step it was in
            phase = getattr(stop, "phase", "prepare")
            for t, kind in unrun:
                _skip(results, _entry(t, kind, env_of_run, args.driver),
                      reason, phase)
                phase = "prepare"
        if torn:
            aborted.append(f"teardown: {_abort_reason(torn)}")
        if aborted:
            results.header["aborted"] = "; ".join(aborted)
        code = _finish(results, ex)
    # why: a Ctrl-C or a SystemExit outlives the report — a shell loop stops
    # only on a child Ctrl-C killed, and tests/run dies of one
    for e in (stop, torn):
        if e is not None and not isinstance(e, Exception):
            raise e
    return code
