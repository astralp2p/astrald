"""Orchestration: selection, suites, execution world by world, results.

The runner owns run state: each world's record, every test's facts, and the
session view each driver and oracle reads. A world owns machines only.
"""
import json
import os
import platform
import re
import subprocess
import sys
import time
import tomllib
import traceback
from dataclasses import dataclass
from pathlib import Path

from astral.client import ENDPOINT_VARS, TOKEN_VARS

from lib import manifest, report, suites
from lib import plan as planner
from lib.build import ensure_binary, git_ref
from lib.executors import ExecutorError
from lib.executors.attach import AttachExecutor
from lib.nodeconfig import PortsBusy, lease_ports
from lib.record import WorldRecord
from lib.results import RunHeader, RunResults, fresh_run_dir
from lib.worlds.local import LocalWorld
from lib.worlds.vm import VmWorld, stages

TESTS = Path(__file__).resolve().parent.parent
REPO = TESTS.parent
RESULTS = TESTS / "results"
# the host's copy of each saved world's record, which --plan --from reads
# without booting anything; the guests hold the copy a run checks against
# why host-wide: netsim's stages are, so every checkout must see one copy
SNAPSHOTS = (Path(os.environ.get("XDG_CACHE_HOME")
                  or Path.home() / ".cache") / "astral-tests" / "snapshots")
# a netsim stage name (netsim store.SLUG_RE); a saved world's host copy is
# named after it too
STAGE_NAME = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
DEFAULT_SUITE = "main.suite"
# the env a record names, by the kind of world it ran in
ENV_OF = {"local": "node", "vm": "netsim", "attach": "node"}
# the CLI options a caller may leave out: the selftests build args by hand
OPTIONS = {"plan": False, "isolate": False, "prereqs": "script", "vm": False,
           "save": None, "from_stage": None}


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


@dataclass
class Step:
    """One step of the run, as the runner executes it."""
    world: str
    test: object        # manifest.Test
    kind: str           # "test", or "prereq": run to build what a test continues
    dir: str            # its artifact directory under results/<stamp>/
    env: str            # the env its world is: "node" or "netsim"
    driver: str         # who performs its flow: "script" or "agent"


def layout(units: list) -> list:
    """Every step of a run of [(world, env, [(Test, kind, driver)])], in
    order, each with the artifact directory it writes.

    why a test's first run keeps results/<stamp>/<test>/: its session view
    then sits two levels under results/, and fs-watch and discover-swarm
    read the run's stamp from that path. A replay of a test the run already
    ran takes <test>.<world> beside it, at the same depth.
    """
    seen, out = set(), []
    for world, env, steps in units:
        for t, kind, driver in steps:
            out.append(Step(world, t, kind,
                            t.name if t.name not in seen else f"{t.name}.{world}",
                            env, driver))
            seen.add(t.name)
    return out


def _entry(s: Step) -> dict:
    """The fields every record of a step carries, whatever its status.

    why env is the world's: a record states the env the test RAN in, not the
    one its manifest asks for as a minimum. A node test lifted into VMs — by
    a netsim test it shares a world with, or by the agent driver — really ran
    there, and a record that said otherwise would hide the one property the
    design is built on.
    """
    return dict(test=s.test.name, kind=s.kind, env=s.env, driver=s.driver,
                world=s.world, artifacts=f"{s.dir}/")


def _skip(results, entry: dict, reason: str, phase: str = "prepare") -> None:
    """Record a test that never finished, with its reason in its driver.log.

    note: a skip never joins the world record's history — nothing ran.
    """
    art = results.dir / entry["artifacts"]
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


def _finish(results, pushed: list) -> int:
    """Write results.json and report.md and print the verdict: the run's
    exit code."""
    # why: a VM verdict is about the code under test only on the machines
    # that run it, so the header names them
    if pushed:
        results.header["pushed"] = ",".join(pushed)
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

    note: this keys off the target, not the header's `hermetic`. A world
    booted --from a saved one is not hermetic, yet its nodes are the
    harness's, not the developer's.
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
    try:
        worlds = plan_worlds(tests, args)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    print(planner.render(worlds), end="")
    return 0


def plan_worlds(tests: dict, args) -> list:
    """The worlds the planner makes of the CLI's selection: a suite's tests,
    with its listing as the order among siblings, or the tests named."""
    if args.target == "attach" and (args.vm or args.save or args.from_stage):
        raise ValueError("--target attach runs the daemon already up: it "
                         "takes no --vm, --save or --from")
    for flag, name in (("--save", args.save), ("--from", args.from_stage)):
        if name is not None and not STAGE_NAME.match(name):
            raise ValueError(f"{flag} {name!r}: a stage name is lowercase "
                             "letters, digits, '.', '_' and '-', starting "
                             "with a letter or a digit, at most 64")
    if args.prereqs != "script" and args.driver != "agent":
        # why refused rather than ignored: only an operator-driven run has
        # prereqs an operator could drive, and the run would read as though
        # one had
        raise ValueError("--prereqs agent needs --driver agent")
    selection = args.selection or [DEFAULT_SUITE]
    listed = selected_suite(selection)
    saved = snapshot(args.from_stage) if args.from_stage else None
    worlds = planner.plan(
        tests, listed or selection, listed or (), isolate=args.isolate,
        driver=args.driver, target=args.target,
        # why --from is a VM run: a saved world is a netsim stage
        vm=args.vm or saved is not None,
        prereqs_agent=args.prereqs == "agent",
        done=[h["test"] for h in saved["history"]] if saved else ())
    if saved is not None:
        _check_saved_config(tests, args.from_stage, saved)
    if args.save and worlds[-1].kind != "vm":
        raise ValueError(f"--save {args.save} saves a VM world, and this "
                         f"selection's last world, {worlds[-1].name}, is "
                         f"{worlds[-1].kind} — add --vm")
    return worlds


def _check_saved_config(tests: dict, name: str, saved: dict) -> None:
    """Refuse a saved world whose history the manifests now configure
    otherwise than when it was saved.

    why: the guests keep the module files the saved world was pushed, and a
    --from run pushes only the overrides it plans, so a key the history no
    longer sets would linger in a guest file unseen.
    note: this reads the host's copy, before anything boots; the boot then
    refuses a guest record that is not exactly that copy, config included.
    """
    planned = {}
    for h in saved["history"]:
        planner.merge_config(planned, tests[h["test"]].config)
    had = saved.get("config", {})
    if json.dumps(planned, sort_keys=True) != json.dumps(had, sort_keys=True):
        raise ValueError(f"--from {name}: the saved world started with node "
                         f"config {json.dumps(had, sort_keys=True)}, and its "
                         "history's manifests now set "
                         f"{json.dumps(planned, sort_keys=True)} — save it "
                         "again")


def snapshot(name: str) -> dict:
    """The world record --save NAME left on this host."""
    path = SNAPSHOTS / f"{name}.json"
    if not path.is_file():
        raise ValueError(f"no saved world {name!r} on this host (a run with "
                         f"--save {name} makes one)")
    return json.loads(path.read_text())


def _check_stages(args) -> None:
    """Refuse a --save that would not land and a --from with nothing to
    boot, before anything is built.

    why --save refuses a taken name: netsim suffixes a taken name rather
    than replacing it, so the world would be saved where no --from looks.
    """
    if not (args.save or args.from_stage):
        return
    try:
        have = stages()
    except ExecutorError as e:
        raise ValueError(str(e)) from e
    if args.save in have:
        raise ValueError(f"--save {args.save}: netsim already holds a stage "
                         "by that name — pick another, or `netsim remove` it")
    if args.from_stage and args.from_stage not in have:
        raise ValueError(f"--from {args.from_stage}: netsim holds no such "
                         "stage")


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
    if args.target == "attach" and run_env(plan, args.driver) == "netsim":
        return "--target attach has no simulation to attach to"
    restarting = [t.name for t, kind in plan if kind == "test" and t.restart]
    if restarting:
        # why refused up front: the driver changes the node before the
        # restart, and only a fresh local world can restart one.
        return (f"{restarting[0]} restarts a node, which only a fresh local "
                "world can do")
    undeclared = [t.name for t, _ in plan if args.driver not in t.drivers]
    if undeclared:
        return f"{undeclared[0]} declares no {args.driver} driver"
    return None


def run_env(plan: list, driver: str = "script") -> str:
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
    if driver == "agent":
        return "netsim"
    selected = [t for t, kind in plan if kind == "test"]
    return "netsim" if any(t.env == "netsim" for t in selected) else "node"


def _executor(args, binary, ref):
    """The machines of the one run the plan does not drive yet: an attached
    daemon."""
    return AttachExecutor()


def _local_world(dir: Path, binary: Path, port_base: int, keep: bool,
                 config: dict | None = None):
    """A fresh local world, its node roots under dir, its nodes started with
    the world's merged config overrides."""
    return LocalWorld(dir, binary, port_base, keep=keep, config=config)


def _vm_world(dir: Path, binary: Path, ref: str, keep: bool,
              config: dict | None = None):
    """A VM world, its logs under dir; it boots on its first test, and pushes
    the world's merged config overrides with the binaries."""
    return VmWorld(dir, binary, ref, keep=keep, config=config)


def attach_plan(tests: dict, args) -> list:
    """[(Test, kind)] for --target attach, or ValueError naming why the
    selection cannot run there.

    todo: child 8 makes attach a world the planner plans (rule 8), and this
    path goes with the executors.
    """
    if (args.isolate or args.prereqs != "script" or args.vm or args.save
            or args.from_stage):
        # why refused rather than ignored: attach runs one daemon that is
        # already up, so none of these can mean anything there
        raise ValueError("--target attach runs the daemon already up: it "
                         "takes no --isolate, --prereqs, --vm, --save or "
                         "--from")
    plan = plan_for(tests, args.selection)
    configured = [t.name for t, _ in plan if t.config]
    if configured:
        # why refused: the attached daemon is not the harness's to configure,
        # so the test would judge a node it did not set up
        raise ValueError(f"{configured[0]} overrides node config, and the "
                         "attached daemon has no harness-rendered config")
    unbuilt = _reject_unbuilt(args, plan)
    if unbuilt:
        raise ValueError(unbuilt)
    return plan


class _Run:
    """What the world loop shares across a run's worlds."""

    def __init__(self, args, tests: dict, results, saved: dict | None,
                 configs: dict):
        self.args, self.tests, self.results = args, tests, results
        self.saved = saved        # the record of the world --from boots
        self.configs = configs    # world -> the overrides it starts with
        self.pushed = []
        self.torn = None          # the first world close that raised
        self.unsaved = None       # why a --save did not land

    def world(self, name: str, steps: list, ex, keep: bool,
              save: str | None = None) -> None:
        """Run one world's steps on ex, save it when asked, then close it."""
        record = WorldRecord(self.results.dir, name)
        record.config = self.configs.get(name, {})
        try:
            if self.saved is not None:
                self._resume(ex, record)
            self._steps(steps, ex, record)
            if save:
                self._save(name, save, ex, record)
        finally:
            # why before the close: a closed world reports no nodes, and a
            # kept one is read back from this record
            _note_nodes(record, ex)
            self.pushed += [vm for vm in ex.pushed if vm not in self.pushed]
            torn = _teardown(ex, keep)
            self.torn = self.torn or torn
            if keep:
                roots = self.results.dir / name / "session"
                print(f"run: --keep, world {name} left up; its nodes are in "
                      f"{record.path}"
                      + (f", node roots and logs under {roots}"
                         if roots.exists() else ""), file=sys.stderr)

    def _resume(self, ex, record: WorldRecord) -> None:
        """Boot the saved world --from names, and take its history and facts
        from the record its guests hold.

        why the guests' record: it is the one that travels with the stage;
        the host's copy only planned the run, and the boot refuses a stage
        whose record is not that copy.
        """
        try:
            saved = ex.resume()
        except ExecutorError:
            return      # note: the boot error fails the world's first step
        record.history = list(saved["history"])
        record.facts = dict(saved["facts"])

    def _save(self, world: str, name: str, ex, record: WorldRecord) -> None:
        """--save: the world into netsim as stage `name`, with its record.

        why only a world whose every step passed: a saved world is reused as
        the state its history names, and one that holds a failed or skipped
        step stands somewhere nobody can name.
        """
        bad = [e["test"] for e in self.results.entries
               if e["world"] == world and e["status"] != "pass"]
        if bad:
            self.unsaved = f"save {name}: {bad[0]} did not pass in {world}"
            return
        _note_nodes(record, ex)
        doc = record.doc()
        try:
            saved = ex.save_world(name, doc)
        except ExecutorError as e:
            self.unsaved = f"save {name}: {e}"
            return
        if saved != name:
            print(f"run: --save {name}: netsim saved it as {saved}",
                  file=sys.stderr)
        SNAPSHOTS.mkdir(parents=True, exist_ok=True)
        (SNAPSHOTS / f"{saved}.json").write_text(
            json.dumps(doc, indent=2) + "\n")
        self.results.header["saved"] = saved

    def _steps(self, steps: list, ex, record: WorldRecord) -> None:
        status, died_at = {}, None
        for s in steps:
            entry = _entry(s)
            # why: a dead node voids every later verdict in its world, and a
            # test whose lineage did not pass stands on a world never built
            reason = (f"world {s.world} died at {died_at}" if died_at
                      else self._blocked(s, status))
            if reason:
                _skip(self.results, entry, reason)
                status[s.test.name] = "skipped"
                continue
            status[s.test.name], died = self._step(s, ex, record, entry)
            died_at = died_at or (s.test.name if died else None)

    def _blocked(self, s: Step, status: dict) -> str | None:
        """Why s cannot run: the first of its lineage that ran in this world
        and did not pass. A lineage test this world never ran — the saved
        world --from boots stands past it — blocks nothing."""
        for name in planner.chain(self.tests, s.test.name)[:-1]:
            if status.get(name, "pass") != "pass":
                return f"{name} failed in {s.world}"
        return None

    def _step(self, s: Step, ex, record: WorldRecord, entry: dict) -> tuple:
        """(status, whether the world died) of one step, recorded."""
        t = s.test
        art = self.results.dir / s.dir
        art.mkdir(parents=True, exist_ok=True)
        t0 = time.monotonic()
        lineage = planner.chain(self.tests, t.name)[-2::-1]   # nearest first
        env = dict(driver_env(art / "session.json", self.args.target),
                   ASTRAL_TESTS_FACTS_OUT=str(art / "facts.json"))
        driven = {}

        def view(phase: str) -> dict:
            # why lineage only: a test sees the facts of the tests its world
            # was built by, never a sibling's. Its oracle also sees its own
            # driver's, read once before the oracle runs, and those are what
            # join the record on a pass.
            facts = record.facts_of(lineage)
            if phase == "verify":
                driven.update(own_facts(art / "facts.json"))
                facts |= driven
            write_view(art / "session.json", record.run, ex.nodes(), facts)
            return env

        status, fk, phase = _perform(ex, t, s.driver, art, view)
        died = ex.broken()
        if died and fk != "environment":
            # why: a dead daemon voids any verdict, even a nominal pass
            status, fk, phase = "fail", "environment", "alive"
        self.results.record(**entry, status=status, failure_kind=fk,
                            phase=phase, duration_s=time.monotonic() - t0)
        record.add(t.name, s.kind, status, driven if status == "pass" else {})
        return status, bool(died)


def _with_defaults(args):
    for name, value in OPTIONS.items():
        if not hasattr(args, name):
            setattr(args, name, value)
    return args


def _units(tests: dict, args) -> tuple:
    """([(world, env, [(Test, kind, driver)])], {world: config}) — what the
    run executes and the node config each world starts with, or ValueError
    naming why it cannot run."""
    if args.target == "attach":
        # note: attach runs through the same world loop and takes its rules;
        # its broken() is always empty, so a world-died skip never fires there
        return [("w0", "node", [(t, kind, args.driver)
                                for t, kind in attach_plan(tests, args)])], {}
    worlds = plan_worlds(tests, args)
    _check_stages(args)
    # note: a step the saved world already stands past is not run, and
    # leaves no record
    return ([(w.name, ENV_OF[w.kind],
              [(tests[s.test], s.kind, s.driver)
               for s in w.steps if not s.done])
             for w in worlds], {w.name: w.config for w in worlds})


def _operator(cfg: dict, results) -> dict:
    """The operator profile config.toml selects for --driver agent.

    why named rather than assumed: the profile IS the operator under test,
    so the run records which one drove it. A pass rate that cannot name its
    agent is not attributable to anything.
    """
    agent_cfg = cfg.get("agent", {})
    name = agent_cfg.get("use", "qwen")
    profile = agent_cfg.get(name)
    if not isinstance(profile, dict):
        raise ValueError(f"config.toml selects agent profile {name!r}, but "
                         f"has no [agent.{name}] section")
    results.header["agent_profile"] = name
    results.header["agent_model"] = profile.get("model", "") or "(lab default)"
    return profile


def main(args) -> int:
    args = _with_defaults(args)
    cfg = tomllib.loads((TESTS / "config.toml").read_text())

    try:
        tests = manifest.load_all(TESTS / "e2e")
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    if args.plan:
        return print_plan(tests, args)
    try:
        units, configs = _units(tests, args)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
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
        # why --from is not hermetic: the world it boots was built by an
        # earlier run, possibly of another astrald
        hermetic=args.target == "fresh" and not args.from_stage,
        selection=" ".join(args.selection) or DEFAULT_SUITE,
        target=args.target))
    if args.from_stage:
        results.header["from"] = args.from_stage
    profile = None
    if args.driver == "agent":
        try:
            profile = _operator(cfg, results)
        except ValueError as e:
            print(f"run: {e}", file=sys.stderr)
            return 2

    lease = None
    if any(env == "node" for _, env, _ in units) and args.target != "attach":
        try:
            # why one lease for the run: its worlds run one after another,
            # so they share a span no other run can take
            lease = lease_ports(**cfg["ports"])
        except PortsBusy as e:
            print(f"run: {e}", file=sys.stderr)
            return 2
    if args.target == "attach":
        ex = _executor(args, binary, ref)
        mutators = [s[0].name for _, _, steps in units for s in steps
                    if s[0].mutates]
        if mutators:
            print(f"run: --target attach: {', '.join(mutators)} really "
                  "mutates this world — it is not a throwaway", file=sys.stderr)

    def open_world(name: str, env: str, keep: bool):
        if args.target == "attach":
            return ex
        if env == "node":
            return _local_world(run_dir / name / "session", binary,
                                lease.base, keep, configs[name])
        world = _vm_world(run_dir / name, binary, ref, keep, configs[name])
        if args.from_stage:
            world.boot_from(args.from_stage, run.saved)
        if profile is not None:
            world.use_operator(profile)
            world.want_model(profile.get("model", ""),
                             profile.get("base_url", ""))
        return world

    steps = layout(units)
    run = _Run(args, tests, results,
               snapshot(args.from_stage) if args.from_stage else None,
               configs)
    stop = None               # what ended the loop early, if anything
    unclosed = None           # the world whose close raised
    try:
        for i, (name, env, _) in enumerate(units):
            # why the last world only: --keep and --save ask for the world
            # the run ends in, and every world before it has already handed
            # on its verdicts
            last = i == len(units) - 1
            keep = args.keep and last
            run.world(name, [s for s in steps if s.world == name],
                      open_world(name, env, keep), keep,
                      args.save if last else None)
            if run.torn is not None:
                if not isinstance(run.torn, Exception):
                    raise run.torn      # Ctrl-C while a world closed
                # why: a world that failed to close may still hold the run's
                # ports, or a simulation still up, and the next world would
                # boot beside it
                unclosed = name
                break
    # why Ctrl-C and every exception: a VM run lasts tens of minutes, and a
    # run that died without a report left only events.jsonl. Whatever ends
    # the loop, the tests it reached keep their verdicts and the rest say why.
    except BaseException as e:
        if isinstance(e, Exception):
            traceback.print_exc()
        stop = e
    finally:
        aborted = []
        # note: the loop records each step once, in run order, so the steps
        # past the last record never finished
        unrun = steps[len(results.entries):]
        if stop is not None or unrun:
            reason = (_abort_reason(stop) if stop is not None
                      else f"{unclosed} failed to close" if unclosed
                      else "the run stopped before its plan ended")
            aborted.append(reason)
            # why: the first step never recorded is the one the stop hit, so
            # it carries the step it was in
            phase = getattr(stop, "phase", "prepare")
            for s in unrun:
                _skip(results, _entry(s), reason, phase)
                phase = "prepare"
        if run.torn is not None:
            aborted.append(f"teardown: {_abort_reason(run.torn)}")
        if run.unsaved is not None:
            aborted.append(run.unsaved)
        if aborted:
            results.header["aborted"] = "; ".join(aborted)
        code = _finish(results, run.pushed)
    # why: a Ctrl-C or a SystemExit outlives the report — a shell loop stops
    # only on a child Ctrl-C killed, and tests/run dies of one
    for e in (stop, run.torn):
        if e is not None and not isinstance(e, Exception):
            raise e
    return code
