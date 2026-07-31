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
