"""Mesh apps: one long-lived `meshapp serve` per node, all on one service.

Each app registers on its node, serves `hello`, advertises the service, follows
it with reach=swarm and calls every provider the follow reports
(tests/e2e/internal/meshapp/README.md holds the state file and the call log).
A driver starts them, the oracle reads what they recorded:

    from lib import meshapps

    names = ["node1", "node2", "node3", "node4"]
    meshapps.spawn("f3", doc["nodes"], names, service="f3", keep_removed=True)
    meshapps.wait_ready("f3", names)
    at, missing = meshapps.wait_answering("f3", names, since=t0, timeout=60)

An app is named `<prefix>-<node name>`; its `App` is `apps.get` of that name.
A pair `(c, p)` is the app on node c calling the app on node p. A pair is
answering since t when its last ok call ended after t.

The functions below `call` are pure: they read states and call logs the
caller passes, and tests/selftest/test_meshapps.py runs them on synthetic ones.
"""
import json
import os
import subprocess
import time
from pathlib import Path

from lib import apps

PACKAGE = "tests/e2e/internal/meshapp"
POLL = 0.5

_binary: Path | None = None


def binary() -> Path:
    """The meshapp binary, built once per process into the world's bin/."""
    global _binary
    if _binary is None or not _binary.exists():
        _binary = apps.build_go(PACKAGE)
    return _binary


def app_name(prefix: str, name: str) -> str:
    return f"{prefix}-{name}"


def _per_app(value, name: str) -> list:
    """A list for every app, or a dict of lists by node name."""
    if value is None:
        return []
    if isinstance(value, dict):
        return list(value.get(name) or [])
    return list(value)


def serve_argv(service: str, *, period: float, keep_removed: bool,
               targets=(), probe_nodes=()) -> list:
    """The `meshapp serve` arguments, without the binary."""
    argv = ["serve", "-service", service, "-period", f"{period}s"]
    if keep_removed:
        argv.append("-keep-removed")
    if targets:
        argv += ["-targets", ",".join(targets)]
    if probe_nodes:
        argv += ["-probe-nodes", ",".join(probe_nodes)]
    return argv


def node_env(node: dict, token: str | None = None) -> dict:
    """The environment that points meshapp at node."""
    env = {"APP_NODE_ENDPOINT": node["endpoint"],
           "APP_NODE_TOKEN": token or node["token"]}
    if node.get("host_ip"):
        env["APP_HOST_IP"] = node["host_ip"]
    return env


def spawn(prefix: str, nodes: dict, names, *, service: str,
          period: float = 1.0, keep_removed: bool = False, targets=(),
          probe_nodes=None) -> dict:
    """Start one app per node name, in one pass; {name: App}.

    `nodes` is the session view's nodes. `targets` and `probe_nodes` are
    identity lists, the same for every app, or dicts of lists by node name.
    """
    exe = binary()
    started = {}
    for name in names:
        argv = serve_argv(service, period=period, keep_removed=keep_removed,
                          targets=_per_app(targets, name),
                          probe_nodes=_per_app(probe_nodes, name))
        started[name] = apps.spawn(app_name(prefix, name), [exe, *argv],
                                   env=node_env(nodes[name]))
    return started


def ready(state: dict) -> bool:
    """Registered, advertised, and the follow reached its boundary or ended."""
    return bool(state.get("registered_at") and state.get("advertised_at")
                and (state.get("follow_opens", 0) >= 1 or state.get("follow_err")))


def wait_ready(prefix: str, names, timeout: float = 30) -> dict:
    """Every app's state once it is ready; apps.AppError when one is not
    within timeout, or dies."""
    deadline = time.monotonic() + timeout
    out = {}
    for name in names:
        left = max(0.0, deadline - time.monotonic())
        out[name] = apps.get(app_name(prefix, name)).wait_state(ready, timeout=left)
    return out


def state_path(prefix: str, name: str) -> Path:
    """The app's state file, `apps.App.state_path` by name.

    why not apps.get: `stop` removes the app's registry entry, and the oracle
    reads the app's last state and call log after it stopped the app.
    """
    return apps.apps_dir() / f"{app_name(prefix, name)}.state.json"


def read_state(path) -> dict:
    """The state file at path; {} when absent or caught mid-write."""
    try:
        return json.loads(Path(path).read_text())
    except (OSError, ValueError):
        return {}


def states(prefix: str, names) -> dict:
    """Every app's last written state, running or stopped; {} for one that
    wrote none."""
    return {n: read_state(state_path(prefix, n)) for n in names}


def calls_path(prefix: str, name: str) -> Path:
    state = state_path(prefix, name)
    return state.with_name(state.name + ".calls.jsonl")


def read_calls(path) -> list:
    """The call log at path; a line caught mid-write is skipped."""
    try:
        text = Path(path).read_text()
    except OSError:
        return []
    out = []
    for line in text.splitlines():
        try:
            out.append(json.loads(line))
        except ValueError:
            continue
    return out


def calls(prefix: str, name: str) -> list:
    """Every finished call of one app, in the order they ended."""
    return read_calls(calls_path(prefix, name))


def stop(prefix: str, names) -> None:
    """Stop the apps; `states` and `calls` still read what they wrote."""
    for name in names:
        apps.get(app_name(prefix, name)).stop()


def call(node: dict, *, target: str, op: str = "hello", token: str | None = None,
         as_app: bool = False, timeout: float = 15) -> dict:
    """One `meshapp call` on node: {caller, target, outcome, code, ms, reply,
    err}. The node's token calls unless token or as_app says otherwise."""
    argv = [str(binary()), "call", "-target", target, "-op", op,
            "-timeout", f"{timeout}s"]
    if as_app:
        argv.append("-as-app")
    p = subprocess.run(argv, capture_output=True, text=True,
                       env={**os.environ, **node_env(node, token)},
                       timeout=timeout + 30)
    lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
    if p.returncode != 0 or not lines:
        raise apps.AppError(f"meshapp call {target}: exit {p.returncode}\n"
                            f"{p.stderr.strip()}")
    return json.loads(lines[-1])


# --- pure functions of states and call logs ---

def ids(states: dict) -> dict:
    """{name: app identity}."""
    return {n: s.get("app") for n, s in states.items()}


def pairs(names) -> list:
    """Every ordered pair of distinct names."""
    return [(c, p) for c in names for p in names if c != p]


def peer(states: dict, c: str, p: str) -> dict | None:
    """What app c recorded of app p, None when it never saw p."""
    return (states[c].get("providers") or {}).get(states[p].get("app"))


def first_calls(states: dict, names) -> dict:
    """{(c, p): c's first finished call to p, or None}."""
    return {(c, p): (peer(states, c, p) or {}).get("first")
            for c, p in pairs(names)}


def answering_since(states: dict, names, t: float) -> list:
    """The pairs not answering since t."""
    out = []
    for c, p in pairs(names):
        last = (peer(states, c, p) or {}).get("last_ok")
        if not last or last["end"] <= t:
            out.append((c, p))
    return out


def first_ok_after(calls: list, to: str, t: float) -> float | None:
    """The end of the first ok call to `to` that ended after t."""
    ends = [e["t1"] for e in calls
            if e.get("to") == to and e.get("outcome") == "ok" and e["t1"] > t]
    return min(ends) if ends else None


def recovered_at(states: dict, logs: dict, names, since: float) -> float | None:
    """The moment every pair answered since `since`: the latest of each
    pair's first ok after it; None while a pair has none. `logs` is
    {name: call log}."""
    app = ids(states)
    ends = [first_ok_after(logs.get(c, []), app[p], since) for c, p in pairs(names)]
    if any(e is None for e in ends):
        return None
    return max(ends, default=since)


def window(calls: list, to: str, start: float, end: float) -> list:
    """The calls to `to` that started in [start, end)."""
    return [e for e in calls if e.get("to") == to and start <= e["t0"] < end]


def wait_answering(prefix: str, names, since: float, timeout: float):
    """Wait up to timeout until every pair answered since `since`;
    (recovered_at, missing pairs). recovered_at is None on a timeout.
    """
    names = list(names)
    deadline = time.monotonic() + timeout
    while True:
        st = states(prefix, names)
        missing = answering_since(st, names, since)
        if not missing:
            logs = {n: calls(prefix, n) for n in names}
            return recovered_at(st, logs, names, since), []
        if time.monotonic() >= deadline:
            return None, missing
        time.sleep(POLL)
