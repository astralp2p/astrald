"""Test manifests (test.toml) and the state chain they compose.

Two kinds of validation, deliberately scoped differently:

* a manifest's own syntax is checked at load and raises immediately — a
  malformed manifest is a repo defect, not a property of one selection;
* chain validation (does anything save this start state? is this suite
  order satisfiable?) is scoped to the tests a selection actually reaches,
  so an unrelated dangling start never breaks a targeted run.
"""
import tomllib
from dataclasses import dataclass
from pathlib import Path

ENVS = {"node", "netsim"}
DRIVERS = {"script", "agent"}
KEYS = {"env", "start", "saves", "mutates", "nodes", "steps", "drivers",
        "timeout", "agent_timeout"}
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


def load_all(e2e_dir: Path) -> dict:
    return {mf.parent.name: _parse(mf)
            for mf in sorted(Path(e2e_dir).glob("*/test.toml"))}


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
                agent_timeout=int(raw.get("agent_timeout", 2400)))


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

    ordered = sorted(needed, key=lambda n: (depths[n], n))
    return [(all[n], "test" if n in selected else "prereq") for n in ordered]


def blocked_by(all: dict, start: str, broken: set) -> str | None:
    """The broken state `start` stands on, if any.

    why: a failure mid-suite skips only what depended on it, and dependence
    is the whole ancestry — a test starting at two-nodes is just as dead as
    one starting at one-node when one-node never got built.
    """
    if not broken:
        return None
    for state in _ancestry(start, _producers(all)):
        if state in broken:
            return state
    return None


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
    # legitimately begin partway up the chain — substrate.suite opens at
    # two-nodes, which the netsim executor materializes from its stage cache
    # rather than builds. Seeding at null would reject it.
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
