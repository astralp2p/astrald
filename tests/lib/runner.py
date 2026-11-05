"""Orchestration: selection, suites, execution world by world, results.

The runner owns run state: each world's record, every test's facts, and the
session view each driver and oracle reads. A world owns machines only.
"""
import json
import os
import platform
import re
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import tomllib
import traceback
from dataclasses import dataclass
from pathlib import Path

from astral.client import ENDPOINT_VARS, TOKEN_VARS

# The variables astral-go's default router reads (lib/apphost/config.go).
GO_ENV_VARS = ("ASTRALD_APPHOST_ENDPOINT", "ASTRALD_APPHOST_TOKEN")

from lib import agent, manifest, report, suites
from lib import plan as planner
from lib.build import ensure_binary, git_ref, linux_binaries
from lib.nodeconfig import PORTS_PER_NODE, PortSpan, PortsBusy, lease_ports
from lib.record import WorldRecord
from lib.results import RunHeader, RunResults, fresh_run_dir
from lib.worlds import WorldError
from lib.worlds.attach import AttachWorld
from lib.worlds import docker
from lib.worlds.docker import DockerWorld
from lib.worlds.local import LocalWorld
from lib.worlds.vm import VmWorld, stages
from lib.worlds.vm import unavailable as vm_unavailable
from lib.worlds.docker import unavailable as docker_unavailable

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
ENV_OF = {"local": "node", "vm": "netsim", "attach": "node",
          "docker": "container"}
# the CLI options a caller may leave out: the selftests build args by hand
OPTIONS = {"plan": False, "isolate": False, "prereqs": "script", "vm": False,
           "docker": False, "save": None, "from_stage": None}
# the skill the operator works with in a Docker world; a VM world's lab bakes
# its own (netsim/ops/configure-astral-agent)
DEFAULT_SKILL = "~/.claude/skills/astral-agent"


class DriverError(Exception):
    """A driver that exited 0 and still left the flow's output broken."""


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


def _perform(ex, t, drive, art: Path, view) -> tuple:
    """(status, failure_kind, phase) of one test: prepare, driver, the
    restart the manifest names, then the oracle. `phase` is the step the
    test stopped at.

    `view(phase)` writes the test's session view for the phase about to run
    and returns the environment that phase runs in. The oracle gets a fresh
    one: a restart changes the nodes, and the oracle also reads what its own
    driver wrote. `drive(env)` performs the flow — script.py or the
    operator — and returns its exit code.

    why one try around every step: an operator's model rewrite, payload push
    or ssh raises from inside the driver step, and a try around prepare alone
    let it leave the loop with no verdict recorded and no report written. A
    WorldError is the world breaking, so it is `environment` at its step.
    A DriverError is a driver that exited 0 and broke its contract, so it is
    `driver` at the driver, whichever step found it.
    """
    phase = "prepare"
    try:
        ex.ensure(t)
        env = view("driver")
        phase = "driver"
        rc = drive(env)
        if rc != 0:
            return "fail", "driver", phase
        if t.restart:
            phase = "restart"
            ex.restart(t.restart)
        phase = "verify"
        rc = _run_step(sys.executable, t.dir / "verify.py",
                       art / "verify.log", view("verify"), t.timeout)
    except WorldError as e:
        with (art / "driver.log").open("a") as f:
            f.write(f"[runner] {e}\n")
        return "fail", "environment", phase
    except DriverError as e:
        with (art / "driver.log").open("a") as f:
            f.write(f"[runner] {e}\n")
        return "fail", "driver", "driver"
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
    env: str            # the env its world is: "node", "container" or "netsim"
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
    one its manifest asks for as a minimum. A node test lifted into a VM or
    Docker world — by a netsim test it shares a world with, by the agent
    driver, or by --vm or --docker — really ran there, and a record that said otherwise would hide the one property the
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


def _close(ex, keep: bool) -> BaseException | None:
    """Tear the world down; the error that stopped it, if any.

    why caught: the report is written after teardown, and a teardown that
    raised used to leave none — or a PASS report above a traceback.
    """
    try:
        ex.close(keep)
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

    why the runner writes it and no world does: the executors this
    replaced wrote three schemas, and only one carried every node field.
    Each test gets its own, in a real directory under results/<stamp>/, so
    the view's path names the run it belongs to.
    """
    doc = {"run": run, "nodes": nodes, "facts": facts}
    path.write_text(json.dumps(doc, indent=2) + "\n")


def _note_nodes(record: WorldRecord, ex) -> None:
    """Write the world's nodes into its record, as the world reports them.

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
    """What a driver wrote with write_facts, or nothing when it wrote none.

    why DriverError: a file that is no JSON object holds no facts an oracle
    can read, and the driver that wrote it failed, not the run.
    """
    if not path.exists():
        return {}
    try:
        facts = json.loads(path.read_text())
    except (OSError, ValueError) as e:
        raise DriverError(f"{path.name} is unreadable: {e}") from e
    if not isinstance(facts, dict):
        raise DriverError(f"{path.name} holds a {type(facts).__name__}, "
                          "not a JSON object")
    return facts


def driver_env(session_json: Path, target: str) -> dict:
    """The environment drivers and oracles run in.

    why: astral-py resolves an endpoint and a token from the ambient
    environment whenever a call passes none, and astral-go's default router
    reads ASTRALD_APPHOST_ENDPOINT and ASTRALD_APPHOST_TOKEN, so a developer's
    own daemon token answers for a test's anonymous connect. Drivers and
    oracles reach every node through session.json alone, so the variables are
    stripped under every target. attach resolves its daemon from them in the
    runner (worlds/attach.py), and the view carries that endpoint and token.

    note: `target` stays a parameter so the header's `hermetic` is never read
    here: a world booted --from a saved one is not hermetic, yet its nodes
    are the harness's, not the developer's.
    """
    env = dict(os.environ,
               PYTHONPATH=os.environ.get("ASTRAL_TESTS_PYPATH", str(TESTS)),
               ASTRAL_TESTS_SESSION=str(session_json))
    for var in ENDPOINT_VARS + TOKEN_VARS + GO_ENV_VARS:
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


def print_plan(tests: dict, args) -> int:
    """--plan: the worlds the planner makes of the selection, printed, with
    nothing built, leased or started."""
    try:
        worlds = plan_worlds(tests, args)
    except ValueError as e:
        print(f"run: {e}", file=sys.stderr)
        return 2
    print(planner.render(worlds), end="")
    # why a note and not a refusal: a plan names what a run would do, on any
    # host, so the goldens regenerate anywhere; the run is what is refused
    why = vm_unavailable()
    vms = [w.name for w in worlds if w.kind == "vm"]
    if why and vms:
        print(f"run: note: this host runs no VM world ({why}); a run of "
              f"this plan is refused at {', '.join(vms)}", file=sys.stderr)
    return 0


def plan_worlds(tests: dict, args) -> list:
    """The worlds the planner makes of the CLI's selection: a suite's tests,
    with its listing as the order among siblings, or the tests named."""
    if args.target == "attach" and (args.vm or args.docker or args.save
                                    or args.from_stage):
        raise ValueError("--target attach runs the daemon already up: it "
                         "takes no --vm, --docker, --save or --from")
    if args.docker and (args.save or args.from_stage):
        raise ValueError("--save and --from work on netsim stages, which a "
                         "Docker world does not make")
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
        vm=args.vm or saved is not None, docker=args.docker,
        heavy=heavy_kind(args),
        prereqs_agent=args.prereqs == "agent",
        done=[h["test"] for h in saved["history"]] if saved else ())
    if saved is not None:
        _check_saved_config(tests, args.from_stage, saved)
    if args.save and worlds[-1].kind != "vm":
        raise ValueError(f"--save {args.save} saves a VM world, and this "
                         f"selection's last world, {worlds[-1].name}, is "
                         f"{worlds[-1].kind} — add --vm")
    return worlds


def heavy_kind(args) -> str:
    """The kind of world the agent driver gets on this host: a VM world
    where netsim runs, a Docker world elsewhere.

    why the host decides, not a flag: an agent run on a laptop should just
    work, and --vm or --docker still say otherwise.
    """
    if args.vm or args.from_stage:
        return "vm"
    return "docker" if vm_unavailable() else "vm"


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
    except WorldError as e:
        raise ValueError(str(e)) from e
    if args.save in have:
        raise ValueError(f"--save {args.save}: netsim already holds a stage "
                         "by that name — pick another, or `netsim remove` it")
    if args.from_stage and args.from_stage not in have:
        raise ValueError(f"--from {args.from_stage}: netsim holds no such "
                         "stage")


def _attach_world():
    """The daemon already up, as the run's one world."""
    return AttachWorld()


def _local_world(dir: Path, binary: Path, span: PortSpan, keep: bool,
                 config: dict | None = None):
    """A fresh local world, its node roots under dir, its nodes started with
    the world's merged config overrides."""
    return LocalWorld(dir, binary, span, keep=keep, config=config)


def _docker_world(dir: Path, binary: Path, image: str, span: PortSpan,
                  keep: bool, config: dict | None = None,
                  skill: Path | None = None):
    """A Docker world, its logs under dir: one container per roster node,
    from `image`, running the Linux `binary`."""
    return DockerWorld(dir, binary, image, span, keep=keep, config=config,
                       skill=skill)


def _prepare_docker() -> tuple:
    """(the Linux astrald, the node image) a run's Docker worlds use, after
    removing what a dead run left; WorldError when the engine fails."""
    for name in docker.reap():
        print(f"run: removed {name}, left by a run that is gone",
              file=sys.stderr)
    binary = linux_binaries(REPO, docker.engine_arch())
    return binary, docker.ensure_image()


def _skill(cfg: dict) -> Path | None:
    """The astral-agent skill a Docker world's operator gets: config.toml's
    [agent] skill, or the usual install; None when there is none."""
    path = Path(os.path.expanduser(cfg.get("agent", {}).get("skill")
                                   or DEFAULT_SKILL))
    if (path / "SKILL.md").is_file():
        return path.resolve()
    return None


def _skill_name(skill: Path | None) -> str:
    """`name version` from a skill's SKILL.md front matter."""
    if skill is None:
        return "(none)"
    fields = {}
    for line in (skill / "SKILL.md").read_text().splitlines()[1:]:
        if line.strip() == "---":
            break
        key, _, value = line.partition(":")
        fields[key.strip()] = value.strip()
    return f"{fields.get('name', '?')} {fields.get('version', '?')}"


def _vm_world(dir: Path, binary: Path, ref: str, keep: bool,
              config: dict | None = None):
    """A VM world, its logs under dir; it boots on its first test, and pushes
    the world's merged config overrides with the binaries."""
    return VmWorld(dir, binary, ref, keep=keep, config=config)


class _Run:
    """What the world loop shares across a run's worlds."""

    def __init__(self, args, tests: dict, results, saved: dict | None,
                 configs: dict, operator: dict | None):
        self.args, self.tests, self.results = args, tests, results
        self.saved = saved        # the record of the world --from boots
        self.configs = configs    # world -> the overrides it starts with
        self.operator = operator  # [agent]'s model and base_url, or None
        self.agent = None         # the operator of the world running now
        self.operated = set()     # the steps its operator drove and passed
        self.pushed = []
        self.torn = None          # the first world close that raised
        self.unsaved = None       # why a --save did not land

    def world(self, name: str, steps: list, ex, keep: bool,
              save: str | None = None) -> None:
        """Run one world's steps on ex, save it when asked, then close it."""
        record = WorldRecord(self.results.dir, name)
        record.config = self.configs.get(name, {})
        # note: the agent driver runs only in VM and Docker worlds (plan rule
        # 5), and those are the worlds that offer ssh()
        self.agent = (agent.Operator(ex.ssh, **self.operator)
                      if self.operator is not None else None)
        # the steps this world's operator drove and passed: their facts
        # files are in its home
        self.operated = set()
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
            torn = _close(ex, keep)
            self.torn = self.torn or torn
            if keep:
                roots = self.results.dir / name / "session"
                print(f"run: --keep, world {name} left up; its nodes are in "
                      f"{record.path}"
                      + (f", node roots and logs under {roots}"
                         if roots.exists() else "")
                      + (f"; containers {', '.join(ex.containers())} "
                         "(docker exec -it <name> sh; docker rm -f to end)"
                         if ex.containers() else ""),
                      file=sys.stderr)

    def _resume(self, ex, record: WorldRecord) -> None:
        """Boot the saved world --from names, and take its history and facts
        from the record its guests hold.

        why the guests' record: it is the one that travels with the stage;
        the host's copy only planned the run, and the boot refuses a stage
        whose record is not that copy.
        """
        try:
            saved = ex.resume()
        except WorldError:
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
        except WorldError as e:
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

        if s.driver == "agent":
            kept = [n for n in reversed(lineage)
                    if self.tests[n].facts and n in self.operated]

            def drive(_env):
                return self.agent.run(t, art, kept=kept)
        else:
            def drive(env):
                return _run_step(sys.executable, t.dir / "script.py",
                                 art / "driver.log", env, t.timeout)
        status, fk, phase = _perform(ex, t, drive, art, view)
        died = ex.dead()
        if died and fk != "environment":
            # why: a dead daemon voids any verdict, even a nominal pass
            status, fk, phase = "fail", "environment", "alive"
        self.results.record(**entry, status=status, failure_kind=fk,
                            phase=phase, duration_s=time.monotonic() - t0)
        record.add(t.name, s.kind, status, driven if status == "pass" else {})
        if s.driver == "agent" and status == "pass":
            self.operated.add(t.name)
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
    why = vm_unavailable()
    if why and args.target == "fresh":
        # why before planning: --save and --from name netsim stages, and a
        # plan of them would answer "add --vm" on a host that cannot
        for flag, given in (("--save", args.save), ("--from", args.from_stage)):
            if given is not None:
                raise ValueError(f"{flag} works on netsim stages, and this "
                                 f"host runs no VM world ({why})")
    worlds = plan_worlds(tests, args)
    if why:
        refusal = _vm_refusal(tests, worlds, args, why)
        if refusal:
            raise ValueError(refusal)
    if any(w.kind == "docker" for w in worlds):
        no_docker = docker_unavailable()
        if no_docker:
            raise ValueError(f"this selection runs in Docker worlds, and "
                             f"this host runs none ({no_docker}); see "
                             "tests/README.md, World kinds")
    _check_stages(args)
    # note: a step the saved world already stands past is not run, and
    # leaves no record
    return ([(w.name, ENV_OF[w.kind],
              [(tests[s.test], s.kind, s.driver)
               for s in w.steps if not s.done])
             for w in worlds], {w.name: w.config for w in worlds})


def unix_socket_trouble() -> str | None:
    """Why this host's temp dir cannot hold an app's unix socket, or None.

    why: astral-py's serve() binds $TMPDIR/apphostclient.<16 chars>, and a
    driver that serves an app fails with nothing pointing at TMPDIR when the
    path passes the OS's sun_path limit (103 characters on macOS)
    why a bind and not a length: the limit differs by OS
    """
    path = (Path(tempfile.gettempdir())
            / f"apphostclient.{secrets.token_hex(8)}")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as s:
        try:
            s.bind(str(path))
        except OSError as e:
            return (f"TMPDIR {tempfile.gettempdir()} is too long for the unix "
                    f"socket a driver's app binds ({len(str(path))} "
                    f"characters: {e}); run with TMPDIR=/tmp")
    path.unlink()
    return None


def _vm_refusal(tests: dict, worlds: list, args, why: str) -> str | None:
    """Why a host that runs no VM world refuses this plan, or None.

    why the whole run: a run that started its local worlds and then failed
    the VM one would read as a red test, not as a host that cannot run it.
    """
    lines = []
    for w in worlds:
        if w.kind != "vm":
            continue
        ops = [(s.test, tests[s.test].ops) for s in w.steps
               if not s.done and tests[s.test].ops]
        if ops:
            lines += [f"  {w.name}: {t} runs VM ops ({'; '.join(o)})"
                      for t, o in ops]
        else:
            # note: the agent driver plans VM worlds only under --vm here;
            # without netsim it gets Docker worlds (heavy_kind)
            lines.append(f"  {w.name}: --vm puts every world in VMs")
    if not lines:
        return None
    return (f"this host runs no VM world ({why}), and this selection needs "
            "one:\n" + "\n".join(lines) + "\nrun it on a Linux host with "
            "netsim, or select without these (tests/README.md, Without "
            "netsim)")


def _operator(cfg: dict, results) -> dict:
    """The model and endpoint config.toml's [agent] names, recorded in the
    header.

    why recorded: a red under one model says nothing about another, so a
    pass rate that cannot name its model is not attributable to anything.
    """
    section = cfg.get("agent", {})
    operator = {"model": section.get("model", ""),
                "base_url": section.get("base_url", "")}
    results.header["agent_profile"] = agent.NAME
    results.header["agent_model"] = operator["model"] or "(lab default)"
    return operator


def main(args) -> int:
    args = _with_defaults(args)
    cfg = tomllib.loads((TESTS / "config.toml").read_text())
    # why from config: a lease holds only its base, and node N of a world
    # takes ports_for(base, N)
    max_nodes = cfg["ports"]["span"] // PORTS_PER_NODE

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

    # why only where a world is local or attached: there the drivers serve
    # apps on this host
    # why before the lease: a refused run leaves no build, lease or results
    trouble = (unix_socket_trouble()
               if any(env == "node" for _, env, _ in units) else None)
    if trouble:
        print(f"run: {trouble}", file=sys.stderr)
        return 2

    lease = None
    containers = any(env == "container" for _, env, _ in units)
    skill = _skill(cfg) if args.driver == "agent" and containers else None
    if args.driver == "agent" and containers and skill is None:
        # why refused: an operator without the skill fails in ways that read
        # as the operator or its prompt breaking, so its verdicts say nothing
        print("run: no astral-agent skill at "
              f"{cfg.get('agent', {}).get('skill') or DEFAULT_SKILL}; set "
              "[agent] skill in tests/config.toml to a satforge/skills "
              "checkout's skills/astral-agent", file=sys.stderr)
        return 2
    # why containers too: a Docker world publishes each node's apphost on a
    # leased host port
    if (containers or any(env == "node" for _, env, _ in units)) \
            and args.target != "attach":
        try:
            # why one lease for the run: its worlds run one after another,
            # so they share a span no other run can take
            # why before the run directory: a refused run leaves none
            lease = lease_ports(**cfg["ports"])
        except PortsBusy as e:
            print(f"run: {e}", file=sys.stderr)
            return 2
    if args.target == "attach":
        binary, ref = None, git_ref(REPO)     # the daemon is already running
    else:
        binary, ref = ensure_binary(REPO)
    linux = image = None
    if containers:
        try:
            # why before the run directory: a host whose engine fails leaves
            # no run behind, as a refused one does
            linux, image = _prepare_docker()
        except (WorldError, subprocess.CalledProcessError) as e:
            print(f"run: the Docker world cannot open: {e}", file=sys.stderr)
            return 2
    run_dir = fresh_run_dir(RESULTS)
    results = RunResults(run_dir, RunHeader(
        astrald_ref=ref,
        astral_py_ref=git_ref(Path(os.path.expanduser(
            cfg["astral_py"]["path"]))),
        host=platform.node(), sandbox="host",
        host_os=f"{platform.system()} {platform.release()}",
        host_arch=platform.machine(),
        # why --from is not hermetic: the world it boots was built by an
        # earlier run, possibly of another astrald
        hermetic=args.target == "fresh" and not args.from_stage,
        selection=" ".join(args.selection) or DEFAULT_SUITE,
        target=args.target))
    if args.from_stage:
        results.header["from"] = args.from_stage
    operator = _operator(cfg, results) if args.driver == "agent" else None
    if operator is not None:
        # why: a verdict under one operator seat says little about another
        results.header["agent_isolation"] = ("container-user" if containers
                                             else "vm-user")
        results.header["agent_version"] = (docker.QWEN_CODE if containers
                                           else "(not recorded)")
        results.header["agent_skill"] = (_skill_name(skill) if containers
                                         else "(not recorded)")

    if args.target == "attach":
        destroyers = [s[0].name for _, _, steps in units for s in steps
                      if s[0].destroys]
        if destroyers:
            print(f"run: --target attach: {', '.join(destroyers)} really "
                  "changes this daemon — it is not a throwaway",
                  file=sys.stderr)

    # why one span for the run: its worlds run one after another on one lease
    span = PortSpan(lease.base, max_nodes) if lease else None

    def open_world(name: str, env: str, keep: bool):
        if args.target == "attach":
            return _attach_world()
        if env == "node":
            return _local_world(run_dir / name / "session", binary,
                                span, keep, configs[name])
        if env == "container":
            return _docker_world(run_dir / name, linux, image, span, keep,
                                 configs[name], skill)
        world = _vm_world(run_dir / name, binary, ref, keep, configs[name])
        if args.from_stage:
            world.boot_from(args.from_stage, run.saved)
        return world

    steps = layout(units)
    run = _Run(args, tests, results,
               snapshot(args.from_stage) if args.from_stage else None,
               configs, operator)
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
