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
        "timeout"}
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
                timeout=int(raw.get("timeout", 120)))


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
    by the fixture prefix their start states need."""
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
    return [(all[n], "test" if n in selected else "fixture") for n in ordered]


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

    state, fence, mutator = NULL, None, None
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
        if t.mutates:
            fence, mutator = t.saves or TERMINAL, name
        state = t.saves or state
