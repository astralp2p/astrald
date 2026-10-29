"""Test manifests (test.toml) and the state chain they compose.

Two kinds of validation, deliberately scoped differently:

* a manifest's own syntax is checked at load and raises immediately — a
  malformed manifest is a repo defect, not a property of one selection;
* chain validation (does anything save this start state? is this suite
  order satisfiable?) is scoped to the tests a selection actually reaches,
  so an unrelated dangling start never breaks a targeted run.
"""
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

ENVS = {"node", "netsim"}
DRIVERS = {"script", "agent"}
KEYS = {"env", "start", "saves", "mutates", "nodes", "steps", "drivers",
        "timeout", "agent_timeout", "unlisted", "restart",
        # the planner's keys (lib/plan.py), beside the state-chain ones above
        "after", "destroys", "ops", "local_only", "facts"}
NULL = "null"

# the walk value after a mutator that saves no state: nothing may follow it
TERMINAL = object()


@dataclass
class Test:
    name: str
    dir: Path
    env: str
    start: str
    saves: str | None
    mutates: bool
    nodes: list
    steps: list
    drivers: list
    timeout: int
    agent_timeout: int
    unlisted: str | None
    restart: list
    # the planner's keys: the test whose world this one continues (None: a
    # fresh world), whether only its own descendants may follow it there, the
    # VM operations before its driver, why it never enters a VM world, the
    # facts its driver writes, and whether an operator can drive it
    after: str | None = None
    destroys: bool = False
    ops: list = field(default_factory=list)
    local_only: str | None = None
    facts: list = field(default_factory=list)
    prompt: bool = False


def load_all(e2e_dir: Path) -> dict:
    all = {mf.parent.name: _parse(mf)
           for mf in sorted(Path(e2e_dir).glob("*/test.toml"))}
    _check_after(all)
    return all


def _check_after(all: dict) -> None:
    """Every `after` names a test, and no chain of them loops.

    why at load and not per selection: unlike a dangling start state, an
    `after` naming nothing is a typo in one file, and the planner walks the
    chain for any selection that reaches it.
    """
    for t in all.values():
        seen, cur = {t.name}, t
        while cur.after is not None:
            if cur.after not in all:
                raise ValueError(f"{cur.name}: after names no test "
                                 f"{cur.after!r}")
            if cur.after in seen:
                raise ValueError(f"{t.name}: after chain loops at "
                                 f"{cur.after!r}")
            seen.add(cur.after)
            cur = all[cur.after]


def _parse(mf: Path) -> Test:
    raw = tomllib.loads(mf.read_text())
    name = mf.parent.name
    unknown = set(raw) - KEYS
    if unknown:
        raise ValueError(f"{name}: unknown manifest key(s) "
                         f"{', '.join(sorted(unknown))}")
    if "env" not in raw:
        raise ValueError(f"{name}: manifest declares no env")
    env = raw["env"]
    if env not in ENVS:
        raise ValueError(f"{name}: bad env {env!r} (one of {sorted(ENVS)})")
    steps = list(raw.get("steps", []))
    if steps and env != "netsim":
        raise ValueError(f"{name}: steps are legal only under env netsim")
    drivers = list(raw.get("drivers", ["script"]))
    bad = set(drivers) - DRIVERS
    if bad or not drivers:
        raise ValueError(f"{name}: bad drivers {sorted(bad) or drivers} "
                         f"(one or both of {sorted(DRIVERS)})")
    nodes = list(raw.get("nodes", []))
    if not nodes:
        raise ValueError(f"{name}: manifest names no nodes")
    restart = list(raw.get("restart", []))
    stray = [n for n in restart if n not in nodes]
    if stray:
        raise ValueError(f"{name}: restart names {stray}, which the roster "
                         f"{nodes} does not")
    after = raw.get("after")
    if after is not None and not (isinstance(after, str) and after):
        raise ValueError(f"{name}: after must name a test")
    if not isinstance(raw.get("destroys", False), bool):
        raise ValueError(f"{name}: destroys must be true or false")
    ops = _strings(name, raw, "ops")
    facts = _strings(name, raw, "facts")
    local_only = _reason(name, raw, "local_only")
    if ops and local_only:
        raise ValueError(f"{name}: a test with ops runs only in a VM world, "
                         "so it cannot be local_only")
    return Test(name=name, dir=mf.parent, env=env,
                start=raw.get("start", NULL),
                saves=raw.get("saves") or None,
                mutates=bool(raw.get("mutates", False)),
                nodes=nodes, steps=steps, drivers=drivers,
                timeout=int(raw.get("timeout", 120)),
                # why its own budget, and why this large: an operator reads a
                # prompt, plans, and works the node through its own shell. A
                # measured bootstrap session spent 80 s in the model and 332 s
                # across 21 shell commands — the VM, not the endpoint, sets
                # the pace, and it was still going when a 900 s budget cut it
                # off. The scripted flow it replaces takes under a second.
                agent_timeout=int(raw.get("agent_timeout", 2400)),
                # why a manifest field and not a list in the selftest: a test
                # no suite lists never runs, and the tree had one such test
                # sitting unexecuted from the day it landed. The reason has to
                # travel with the test, so removing the last suite entry that
                # named it fails rather than silently retiring it.
                unlisted=_reason(name, raw, "unlisted"),
                # why between driver and oracle: a driver leaves state on a
                # node, and the oracle judges what the node holds after it
                # boots again. A driver cannot relaunch a node itself — the
                # executor owns the process, and a node it did not start
                # reads to the runner as a dead one.
                restart=restart,
                after=after,
                # why ops imply it: an op reshapes the VM world in place
                # (enter-nat, leave-lan), and nothing puts it back
                destroys=bool(raw.get("destroys", False)) or bool(ops),
                ops=ops,
                local_only=local_only,
                facts=facts,
                prompt=(mf.parent / "prompt.md").is_file())


def _strings(name: str, raw: dict, key: str) -> list:
    """A key whose value is a list of non-empty strings, or absent."""
    value = raw.get(key, [])
    if not (isinstance(value, list)
            and all(isinstance(v, str) and v.strip() for v in value)):
        raise ValueError(f"{name}: {key} must be a list of non-empty strings")
    return list(value)


def _reason(name: str, raw: dict, key: str) -> str | None:
    """A key whose value is a reason: absent, or a non-empty string."""
    if key not in raw:
        return None
    reason = raw[key]
    if not isinstance(reason, str) or not reason.strip():
        raise ValueError(f"{name}: {key} must be a non-empty reason string")
    return reason.strip()


def _producers(all: dict) -> dict:
    """state -> the one test that saves it. A second producer would silently
    shadow the first, so the state maps to None and consulting it raises."""
    out = {}
    for name in sorted(all):
        t = all[name]
        if t.saves:
            out[t.saves] = None if t.saves in out else t
    return out


def _producer(producers: dict, state: str) -> Test:
    if state not in producers:
        raise ValueError(f"no test saves state {state!r}")
    t = producers[state]
    if t is None:
        raise ValueError(f"state {state!r} has more than one producer")
    return t


def _ancestry(state: str, producers: dict) -> list:
    """state, then the state it was built from, … down to null."""
    chain, seen = [], set()
    while state != NULL:
        if state in seen:
            raise ValueError(f"start/saves cycle at state {state!r}")
        seen.add(state)
        chain.append(state)
        state = _producer(producers, state).start
    chain.append(NULL)
    return chain


def _reaches(candidate: str, state: str, producers: dict) -> bool:
    """True when candidate is state or one of the states state was built on."""
    return candidate in _ancestry(state, producers)


def resolve(all: dict, only) -> list:
    """Execution plan for a bare test selection: the selected tests, preceded
    by the prereq prefix their start states need."""
    producers = _producers(all)

    if only is None:
        selected = set(all)
    else:
        unknown = [n for n in only if n not in all]
        if unknown:
            raise ValueError(f"unknown test(s): {', '.join(unknown)}")
        selected = set(only)

    needed = set(selected)
    frontier = list(selected)
    depths = {}
    while frontier:
        t = all[frontier.pop()]
        depths[t.name] = len(_ancestry(t.start, producers)) - 1
        if t.start != NULL:
            dep = _producer(producers, t.start).name
            if dep not in needed:
                needed.add(dep)
                frontier.append(dep)

    # why this key: a mutator invalidates the state its siblings start from,
    # so it runs last; and at equal depth a test that builds nothing runs
    # before a producer, so claim-window still finds node1 unclaimed.
    ordered = sorted(needed, key=lambda n: (all[n].mutates, depths[n],
                                            bool(all[n].saves), n))
    return [(all[n], "test" if n in selected else "prereq") for n in ordered]


def validate_order(all: dict, names: list) -> None:
    """Reject a suite whose order cannot satisfy the chain.

    The walk carries the state the listed tests have built so far. A test is
    placeable when its start is that state or one of its ancestors — strict
    equality is wrong, since expel-node (start two-nodes) legitimately
    follows object-store (walk value two-nodes-data).

    why: ancestry alone cannot fence a mutator — two-nodes is an ancestor of
    two-nodes-expel, yet nothing may consume two-nodes once expel-node has
    run. A mutator therefore also narrows the walk to its own branch.
    """
    producers = _producers(all)
    unknown = [n for n in names if n not in all]
    if unknown:
        raise ValueError(f"unknown test(s): {', '.join(unknown)}")

    # why: the walk seeds at the first test's start, not at null. A suite may
    # legitimately begin partway up the chain — a suite run against a pinned
    # stage opens at whatever state that stage stands at, which the netsim
    # executor boots rather than builds. Seeding at null would reject it.
    state, fence, mutator = all[names[0]].start, None, None
    for name in names:
        t = all[name]
        if fence is TERMINAL:
            raise ValueError(
                f"{name}: nothing may follow {mutator!r}, which mutates the "
                f"chain and saves no state of its own")
        if fence is not None and not _reaches(fence, t.start, producers):
            raise ValueError(
                f"{name}: start {t.start!r} is not on the {fence!r} branch — "
                f"{mutator!r} mutated it earlier in this suite")
        if not _reaches(t.start, state, producers):
            raise ValueError(
                f"{name}: start {t.start!r} is not reachable from {state!r} — "
                f"this suite order cannot satisfy the chain")
        # why: an ancestor start is legal, but re-running a producer is not.
        # This is what catches a misordered suite now that the walk no longer
        # seeds at null: adopt-node before bootstrap-user-software-key leaves
        # bootstrap saving a one-node the walk already stands on.
        if t.saves and _reaches(t.saves, state, producers):
            raise ValueError(
                f"{name}: state {t.saves!r} is already built at this point — "
                f"this suite order cannot satisfy the chain")
        if t.mutates:
            fence, mutator = t.saves or TERMINAL, name
        state = t.saves or state
