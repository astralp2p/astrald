"""Test manifests (test.toml): what each test needs, in one file per test.

A manifest's syntax is checked at load and raises immediately — a malformed
manifest is a repo defect, not a property of one selection. What a selection
does with the tests is the planner's (lib/plan.py).
"""
import math
import re
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

KEYS = {"nodes", "timeout", "restart", "stop", "after", "destroys", "ops",
        "local_only", "host_listener", "facts", "config"}
MODULE = re.compile(r"[a-z][a-z0-9_]*")

# (module, key) the harness owns: the ports a world leases, the token the
# session view hands drivers, the authority it points drivers at, and the
# apphost HTTP listener it turns off.
# why refused: an override of one breaks what the session view promises — a
# replaced token leaves a node no driver can reach, and a moved port sends
# drivers to nothing. mcp.bind_mcp may only be emptied, since mcp_url names
# the leased port. bind_http names a port no lease covers, so two runs, or
# a developer's own daemon, would bind it at once.
HARNESS_KEYS = {("apphost", "listen"), ("apphost", "tokens"),
                ("apphost", "bind_http"),
                ("tcp", "listen_port"), ("ether", "udp_port"),
                ("kcp", "listen_port"), ("auth", "external_authorizers")}


@dataclass
class Test:
    name: str
    dir: Path
    nodes: list
    timeout: int
    restart: list
    # nodes the world stops after the driver; no later test reaches them
    stop: list = field(default_factory=list)
    # the test whose world this one continues (None: a fresh world), whether
    # only its own descendants may follow it there, the VM operations before
    # its driver, why it never enters a VM world, the facts its driver
    # writes, and whether an operator can drive it (it ships a prompt.md)
    after: str | None = None
    destroys: bool = False
    ops: list = field(default_factory=list)
    local_only: str | None = None
    # why a node dials a listener the test serves on the test host (at the
    # node's host_ip); a VM world gives its nodes no such address
    host_listener: str | None = None
    facts: list = field(default_factory=list)
    prompt: bool = False
    # whether it ships a while_down.py, which its restart runs between
    # stopping the nodes and starting them
    while_down: bool = False
    # node -> module -> keys: what its world's nodes start with, on top of
    # what lib/nodeconfig builds
    config: dict = field(default_factory=dict)


def load_all(e2e_dir: Path) -> dict:
    all = {mf.parent.name: _parse(mf)
           for mf in sorted(Path(e2e_dir).glob("*/test.toml"))}
    _check_after(all)
    return all


def _check_after(all: dict) -> None:
    """Every `after` names a test, and no chain of them loops.

    why at load and not per selection: an `after` naming nothing is a typo
    in one file, and the planner walks the chain for any selection that
    reaches it.
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
    nodes = list(raw.get("nodes", []))
    if not nodes:
        raise ValueError(f"{name}: manifest names no nodes")
    restart = list(raw.get("restart", []))
    stray = [n for n in restart if n not in nodes]
    if stray:
        raise ValueError(f"{name}: restart names {stray}, which the roster "
                         f"{nodes} does not")
    stop = _strings(name, raw, "stop")
    stray = [n for n in stop if n not in nodes]
    if stray:
        raise ValueError(f"{name}: stop names {stray}, which the roster "
                         f"{nodes} does not")
    both = sorted(set(stop) & set(restart))
    if both:
        raise ValueError(f"{name}: {both} cannot be both restarted and "
                         "stopped")
    while_down = (mf.parent / "while_down.py").is_file()
    if while_down and not restart:
        raise ValueError(f"{name}: ships a while_down.py and restarts no "
                         "node, so nothing would run it")
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
    host_listener = _reason(name, raw, "host_listener")
    if ops and host_listener:
        raise ValueError(f"{name}: a test with ops runs only in a VM world, "
                         "which has no host_ip, so it cannot be "
                         "host_listener")
    return Test(name=name, dir=mf.parent, nodes=nodes,
                timeout=int(raw.get("timeout", 120)),
                # why between driver and oracle: a driver leaves state on a
                # node, and the oracle judges what the node holds after it
                # boots again. A driver cannot relaunch a node itself — the
                # world owns the process, and a node it did not start reads
                # to the runner as a dead one.
                restart=restart,
                # why after the driver: the driver leaves the state the
                # stopped node took part in, and the tests after this one
                # judge a world where that node is gone. A driver cannot
                # stop a node itself, for the reason it cannot restart one.
                stop=stop,
                after=after,
                # why ops imply it: an op reshapes the VM world in place
                # (enter-nat, leave-lan), and nothing puts it back
                # why stop implies it: a test sharing the world that names
                # the stopped node would find it gone
                destroys=(bool(raw.get("destroys", False)) or bool(ops)
                          or bool(stop)),
                ops=ops,
                local_only=local_only,
                host_listener=host_listener,
                facts=facts,
                # why a file and not a key: a test an operator can drive is
                # exactly one that tells it what to do
                prompt=(mf.parent / "prompt.md").is_file(),
                # why a file and not a key: like the driver and the oracle,
                # it is a script the test ships
                while_down=while_down,
                config=_config(name, raw, nodes))


def _strings(name: str, raw: dict, key: str) -> list:
    """A key whose value is a list of non-empty strings, or absent."""
    value = raw.get(key, [])
    if not (isinstance(value, list)
            and all(isinstance(v, str) and v.strip() for v in value)):
        raise ValueError(f"{name}: {key} must be a list of non-empty strings")
    return list(value)


def _config(name: str, raw: dict, nodes: list) -> dict:
    """[config.<node>.<module>] tables: node -> module -> keys.

    why the roster is checked: an override for a node the test never starts
    would change a node it shares with another test, behind that test's back.
    """
    value = raw.get("config", {})
    shape = f"{name}: config must be [config.<node>.<module>] tables of keys"
    if not isinstance(value, dict):
        raise ValueError(shape)
    for node, modules in value.items():
        if node not in nodes:
            raise ValueError(f"{name}: config names {node!r}, which the "
                             f"roster {nodes} does not")
        if not isinstance(modules, dict) or not modules:
            raise ValueError(shape)
        for module, keys in modules.items():
            if not MODULE.fullmatch(module) or not isinstance(keys, dict) \
                    or not keys:
                raise ValueError(shape)
            for key, v in keys.items():
                where = f"config.{node}.{module}.{key}"
                if (module, key) in HARNESS_KEYS or (
                        (module, key) == ("mcp", "bind_mcp") and v != ""):
                    raise ValueError(f"{name}: {where} is the harness's to "
                                     "set (mcp.bind_mcp may only be \"\")")
                _check_value(name, where, v)
    return value


def _check_value(name: str, where: str, v) -> None:
    """A value a module's config file can hold: no TOML date or time."""
    if isinstance(v, dict):
        for k, sub in v.items():
            _check_value(name, f"{where}.{k}", sub)
    elif isinstance(v, list):
        for sub in v:
            _check_value(name, where, sub)
    elif not isinstance(v, (str, bool, int, float)):
        raise ValueError(f"{name}: {where} is a {type(v).__name__}, which "
                         "no module config holds")
    elif isinstance(v, float) and not math.isfinite(v):
        raise ValueError(f"{name}: {where} is {v}, which JSON cannot hold")


def _reason(name: str, raw: dict, key: str) -> str | None:
    """A key whose value is a reason: absent, or a non-empty string."""
    if key not in raw:
        return None
    reason = raw[key]
    if not isinstance(reason, str) or not reason.strip():
        raise ValueError(f"{name}: {key} must be a non-empty reason string")
    return reason.strip()
