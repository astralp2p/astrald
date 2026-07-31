"""--target attach: no spawning at all — run against a daemon already up.

The code-and-debug loop. Point a test at the astrald you are hacking on and
let the standard oracle judge it, with open eyes: nothing here is hermetic,
nothing is torn down, and a state-mutating test really mutates your daemon.
"""
import asyncio
from pathlib import Path

import astral
from astral.client import resolve_endpoint, resolve_token

from lib.executors import Executor, ExecutorError


class AttachExecutor(Executor):
    env = "node"

    def __init__(self, dir: Path):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.facts = {}
        self._node = None

    @property
    def session_json_path(self) -> Path:
        return self.dir / "session.json"

    def prepare(self, test) -> None:
        if test.steps:
            raise ExecutorError(f"{test.name}: attach runs no steps")
        if len(test.nodes) != 1:
            # why: the roster comes from astral-py's ambient resolution, which
            # names exactly one daemon. A second roster name has nothing to
            # resolve to, and quietly pointing both at one daemon would let
            # adopt-node "pass" by adopting a node into itself.
            raise ExecutorError(
                f"{test.name}: --target attach has one daemon and this test "
                f"wants {len(test.nodes)} ({', '.join(test.nodes)})")
        if self._node is None:
            self._node = asyncio.run(self._probe(test.nodes[0]))
        self._write_session()

    async def _probe(self, name: str) -> dict:
        endpoint, token = resolve_endpoint(), resolve_token()
        try:
            async with await astral.connect(endpoint, token=token) as c:
                identity = str(await c.apphost.whoami()) if c.authenticated \
                    else ""
                if not await c.shell.spec():
                    raise ExecutorError(f"{endpoint}: empty op catalog")
        except astral.AstralError as e:
            raise ExecutorError(f"{endpoint}: {e}") from e
        return {"name": name, "endpoint": endpoint, "token": token or "",
                "identity": identity}

    def merge_facts(self, path: Path) -> None:
        import json
        if Path(path).exists():
            self.facts.update(json.loads(Path(path).read_text()))
            self._write_session()

    def save(self, state: str) -> None:
        """Nothing to snapshot: the daemon is the operator's, not the run's."""

    def broken(self) -> list:
        return []

    def teardown(self, keep: bool) -> None:
        """Never stops the daemon. It was running before this run and stays."""

    def _write_session(self) -> None:
        import json
        n = self._node
        doc = {"nodes": {n["name"]: {
            "endpoint": n["endpoint"], "token": n["token"],
            "identity": n["identity"], "root": "attached",
            "tcp_port": 0,
        }}, "facts": self.facts}
        self.session_json_path.write_text(json.dumps(doc, indent=2) + "\n")
