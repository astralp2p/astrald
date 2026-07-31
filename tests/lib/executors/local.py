"""env node: fresh astrald processes on loopback, one live session per run."""
import asyncio
from pathlib import Path

from lib.executors import Executor, ExecutorError
from lib.session import Session


class LocalExecutor(Executor):
    env = "node"

    def __init__(self, dir: Path, binary: Path, port_base: int):
        self._session = Session(dir, binary, port_base)

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

    def broken(self) -> list:
        return self._session.dead_nodes()

    def teardown(self, keep: bool) -> None:
        if not keep:
            self._session.teardown()
