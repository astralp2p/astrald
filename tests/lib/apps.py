"""Long-lived app processes that outlive the driver that started them.

A driver's own processes die with it, and `restart = [...]` in a manifest
restarts nodes between the driver and the oracle. A test about an app that
survives a node restart therefore needs an app process the harness owns: a
driver starts it with `spawn`, the driver exits, the world restarts a node, and
the oracle finds the same process still running with `get`. The runner stops
every app of a world before it closes the world's nodes, on a pass, a failure,
Ctrl-C and SIGTERM alike (lib/runner.py, `_Run.world`).

    from lib import apps

    binary = apps.build_go("tests/e2e/my-test/driver")        # once per script
    app = apps.spawn("my-test-provider", [str(binary), "serve"],
                     env={"APP_NODE_ENDPOINT": ..., "APP_NODE_TOKEN": ...})
    state = app.wait_state(lambda s: s.get("attached"), timeout=30)
    ...                                                        # driver ends
    app = apps.get("my-test-provider")                         # in the oracle
    assert app.alive()
    app.stop()

The process and the script share two things. `ASTRAL_TESTS_APP_STATE` names a
JSON file the app rewrites (whole file, by rename) whenever it has news: what
`App.state` and `App.wait_state` read, so the app reports readiness and what
it served with no side channel. Its stdout and stderr go to a log beside it,
kept in the run's results.

Everything lives in one directory per world, `ASTRAL_TESTS_APPS`, which the
runner sets for each step: `<name>.app.json` (pid, start time, argv),
`<name>.state.json`, `<name>.log`, and `bin/` for `build_go`. Names are
world-wide, so a test prefixes its own.

An app runs in a session of its own, so the process group is what stops: the
app and anything it started. A SIGKILLed runner cannot reap; it leaves the apps
running and their entries in the results directory (a kept world does the same
on purpose).
"""
import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

ENV_DIR = "ASTRAL_TESTS_APPS"
ENV_STATE = "ASTRAL_TESTS_APP_STATE"

REPO = Path(__file__).resolve().parents[2]
STOP_GRACE = 8.0           # seconds between SIGTERM and SIGKILL

# the children this process spawned, so a dead one is waited for and never
# mistaken for a live zombie
_children: dict[int, subprocess.Popen] = {}


class AppError(Exception):
    pass


def apps_dir() -> Path:
    """The world's app directory, created. AppError outside a run."""
    d = os.environ.get(ENV_DIR)
    if not d:
        raise AppError(f"{ENV_DIR} is not set: apps run only under tests/run")
    path = Path(d)
    path.mkdir(parents=True, exist_ok=True)
    return path


def _started(pid: int) -> str | None:
    """The kernel's start time of pid, the key that tells a process from a
    later one with the same pid; None where /proc does not say."""
    try:
        stat = Path(f"/proc/{pid}/stat").read_text()
    except OSError:
        return None
    # the command name may hold spaces and parentheses: fields count from the
    # last ')' — state is field 3, start time field 22
    fields = stat[stat.rindex(")") + 2:].split()
    return fields[19] if len(fields) > 19 else None


def _zombie(pid: int) -> bool:
    try:
        stat = Path(f"/proc/{pid}/stat").read_text()
    except OSError:
        return False
    return stat[stat.rindex(")") + 2:][:1] == "Z"


def _alive(pid: int, started: str | None) -> bool:
    child = _children.get(pid)
    if child is not None:
        if child.poll() is not None:
            _children.pop(pid, None)
            return False
        return True
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return False         # not ours: a pid reused by another user's process
    if started is not None and _started(pid) != started:
        return False
    return not _zombie(pid)


class App:
    """A registered app process, by name."""

    def __init__(self, dir: Path, name: str, pid: int, started: str | None,
                 argv: list):
        self.dir, self.name, self.pid = Path(dir), name, pid
        self.started, self.argv = started, argv

    @property
    def log(self) -> Path:
        return self.dir / f"{self.name}.log"

    @property
    def state_path(self) -> Path:
        return self.dir / f"{self.name}.state.json"

    @property
    def entry(self) -> Path:
        return self.dir / f"{self.name}.app.json"

    def alive(self) -> bool:
        return _alive(self.pid, self.started)

    def state(self) -> dict:
        """What the app last wrote to its state file; {} before it wrote one."""
        try:
            return json.loads(self.state_path.read_text())
        except (OSError, ValueError):
            return {}           # absent, or caught mid-write by a non-atomic app

    def log_tail(self, lines: int = 20) -> str:
        try:
            return "\n".join(self.log.read_text(errors="replace")
                             .splitlines()[-lines:])
        except OSError:
            return ""

    def wait_state(self, ready, timeout: float = 30.0,
                   poll: float = 0.2) -> dict:
        """The app's state once `ready(state)` is true, waited for up to
        `timeout` seconds. AppError, with the log's tail, when the app dies
        first or the time runs out."""
        deadline = time.monotonic() + timeout
        while True:
            state = self.state()
            if state and ready(state):
                return state
            if not self.alive():
                raise AppError(f"{self.name} exited before it was ready; "
                               f"log {self.log}:\n{self.log_tail()}")
            if time.monotonic() >= deadline:
                raise AppError(f"{self.name} was not ready after "
                               f"{timeout:.0f}s (state {state}); log "
                               f"{self.log}:\n{self.log_tail()}")
            time.sleep(poll)

    def stop(self, grace: float = STOP_GRACE) -> None:
        """Stop the app's whole process group: SIGTERM, then SIGKILL after
        `grace` seconds. Stopping a stopped app is not an error."""
        if self.alive():
            _signal_group(self.pid, signal.SIGTERM)
            deadline = time.monotonic() + grace
            while self.alive() and time.monotonic() < deadline:
                time.sleep(0.05)
        # why always: a child of the app may outlive its leader
        _signal_group(self.pid, signal.SIGKILL)
        _children.pop(self.pid, None)
        self.entry.unlink(missing_ok=True)


def _signal_group(pgid: int, sig: int) -> None:
    try:
        os.killpg(pgid, sig)
    except ProcessLookupError:
        pass


def build_go(package: str, name: str | None = None) -> Path:
    """Build the Go package at `package` (a path under the repository, as
    `go build` takes it) into the world's `bin/` and return the binary.

    why a binary and not `go run`: the app's pid must be the app's own, and a
    `go run` parent would be the process the harness signals.
    """
    name = name or Path(package).name
    out = apps_dir() / "bin" / name
    out.parent.mkdir(parents=True, exist_ok=True)
    env = {**os.environ,
           "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")}
    p = subprocess.run(["go", "build", "-o", str(out), f"./{package}"],
                       cwd=REPO, capture_output=True, text=True, env=env)
    if p.returncode != 0:
        raise AppError(f"go build {package}: exit {p.returncode}\n{p.stderr}")
    return out


def spawn(name: str, argv: list, *, env: dict | None = None,
          cwd: str | os.PathLike | None = None) -> App:
    """Start `argv` as the long-lived app `name`, and return it.

    The process gets the caller's environment, `env` over it, and
    ASTRAL_TESTS_APP_STATE. It outlives the script that called `spawn`.
    Secrets go in `env`: the registry records argv.
    """
    d = apps_dir()
    if (live := _lookup(d, name)) is not None and live.alive():
        raise AppError(f"app {name} is already running (pid {live.pid})")
    state = d / f"{name}.state.json"
    state.unlink(missing_ok=True)
    with (d / f"{name}.log").open("wb") as log:
        proc = subprocess.Popen(
            [str(a) for a in argv], cwd=cwd, stdin=subprocess.DEVNULL,
            stdout=log, stderr=subprocess.STDOUT,
            env={**os.environ, **(env or {}), ENV_STATE: str(state)},
            # why: a signal sent to the runner's process group must not reach
            # the app; the world's teardown stops it, and the group is what
            # that teardown signals
            start_new_session=True)
    _children[proc.pid] = proc
    app = App(d, name, proc.pid, _started(proc.pid), [str(a) for a in argv])
    app.entry.write_text(json.dumps(
        {"name": name, "pid": app.pid, "started": app.started,
         "argv": app.argv}) + "\n")
    return app


def _lookup(d: Path, name: str) -> App | None:
    try:
        e = json.loads((d / f"{name}.app.json").read_text())
    except (OSError, ValueError):
        return None
    return App(d, e["name"], e["pid"], e.get("started"), e.get("argv", []))


def get(name: str) -> App:
    """The app `name` an earlier script of this world spawned, running or
    not. AppError when none was."""
    app = _lookup(apps_dir(), name)
    if app is None:
        raise AppError(f"no app {name} was spawned in this world")
    return app


def reap(d: str | os.PathLike) -> list:
    """Stop every app registered in directory `d`; the names of those that
    were still running. The runner calls it before a world's nodes stop.

    Never raises for one app that will not stop: the others are stopped
    first, and the failures are raised together.
    """
    d = Path(d)
    running, failed = [], []
    for entry in sorted(d.glob("*.app.json")) if d.is_dir() else []:
        app = _lookup(d, entry.name.removesuffix(".app.json"))
        if app is None:
            continue
        try:
            if app.alive():
                running.append(app.name)
            app.stop()
        except Exception as e:                    # noqa: BLE001
            failed.append(f"{app.name}: {e}")
    if failed:
        raise AppError("; ".join(failed))
    return running


def stop_all() -> list:
    """Stop every app of this world: `reap` of the world's own directory."""
    return reap(apps_dir())


if __name__ == "__main__":
    sys.exit("lib/apps.py is a library: from lib import apps")
