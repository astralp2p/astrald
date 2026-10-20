"""env node: fresh astrald processes on loopback, one live session per run."""
import asyncio
import shutil
import tempfile
from pathlib import Path

from lib.executors import Executor, ExecutorError
from lib.nodeconfig import PortLease
from lib.session import Session


class LocalExecutor(Executor):
    env = "node"

    def __init__(self, dir: Path, binary: Path, ports: PortLease,
                 keep: bool = False):
        self._ports = ports   # held for the run: no other run takes the span
        # why: the nodes write their state under the system temp dir, and dir
        # is a symlink to it until teardown copies the world back. astrald
        # fsyncs its SQLite database, and on an SD card under write load one
        # fsync stalls for 20-30 s: a node then misses the 30 s readiness wait
        # or a driver's deadline. Where the temp dir is tmpfs, as /tmp is on
        # ant3, no fsync reaches the card.
        self._dir = Path(dir)
        self._live = Path(tempfile.mkdtemp(prefix="astrald-tests-"))
        self._dir.parent.mkdir(parents=True, exist_ok=True)
        self._dir.symlink_to(self._live, target_is_directory=True)
        self._session = Session(self._dir, binary, ports.base, keep=keep)

    @property
    def session_json_path(self) -> Path:
        return self._session.session_json_path

    def prepare(self, test) -> None:
        if test.steps:
            raise ExecutorError(f"{test.name}: env node runs no steps")
        try:
            asyncio.run(self._session.ensure(test.nodes))
        except Exception as e:
            raise ExecutorError(f"session: {e}") from e

    def merge_facts(self, path: Path) -> None:
        self._session.merge_facts(path)

    def save(self, state: str) -> None:
        """No-op: the live session already is the state, and the next test in
        the chain inherits it in place. Only netsim snapshots."""

    def restart(self, names: list) -> None:
        try:
            asyncio.run(self._session.restart(names))
        except Exception as e:
            raise ExecutorError(f"restart: {e}") from e

    def broken(self) -> list:
        return self._session.dead_nodes()

    def teardown(self, keep: bool) -> None:
        """Stop the nodes and move their state from the temp dir into dir.

        A kept world stays where it runs, still reached through dir."""
        if keep:
            return
        self._session.teardown()
        self._dir.unlink()
        shutil.copytree(self._live, self._dir)
        shutil.rmtree(self._live)
