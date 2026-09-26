"""Spawn and manage one isolated astrald instance on loopback."""
import asyncio
import ctypes
import re
import secrets
import signal
import subprocess
import sys
from pathlib import Path

import astral

from lib.nodeconfig import WITHOUT_MCP, NodePorts, render

# startup line prints the identity in parens; astrald's earliest lines are
# raw %v dumps, so the hex can carry internal padding: "( <66-hex> )"
_IDENT_RE = re.compile(r"\(\s*([0-9a-f]{66})\s*\)")


class NodeError(Exception):
    pass


def _die_with_runner() -> None:
    """Runs in the daemon before exec: the kernel sends it SIGTERM when the
    runner exits, however the runner exits.

    note: the kernel ties this to the thread that spawns the daemon, which is
    the runner's main thread.
    """
    PR_SET_PDEATHSIG = 1
    ctypes.CDLL(None).prctl(PR_SET_PDEATHSIG, signal.SIGTERM)


class LocalNode:
    def __init__(self, name: str, root: Path, binary: Path, ports: NodePorts):
        self.name = name
        self.root = Path(root)
        self.binary = Path(binary)
        self.ports = ports
        self.token = secrets.token_hex(16)
        self.serves_mcp = name not in WITHOUT_MCP
        self.identity = None
        self.proc = None
        self.log_path = self.root / "astrald.log"

    @property
    def endpoint(self) -> str:
        return f"tcp:127.0.0.1:{self.ports.apphost}"

    def start(self, keep: bool = False) -> None:
        render(self.root, self.ports, self.token, self.serves_mcp)
        self.root.mkdir(parents=True, exist_ok=True)
        log = self.log_path.open("ab")
        # why: a daemon in the runner's process group receives every signal
        # sent to that group, and a SIGINT during boot killed a node the run
        # then reported as a broken environment. stop() signals the daemon
        # itself, and the parent-death signal replaces the group's reach when
        # the runner dies without a teardown.
        #
        # why: --keep asks for the opposite — a world that outlives the runner
        # — so the parent-death signal is not installed for a kept run. The
        # no-keep path is unchanged and still leaves no daemon behind a
        # SIGKILLed runner.
        self.proc = subprocess.Popen(
            [str(self.binary), "-root", str(self.root)],
            stdout=log, stderr=subprocess.STDOUT, start_new_session=True,
            preexec_fn=None if keep or sys.platform != "linux"
            else _die_with_runner)

    def alive(self) -> bool:
        return self.proc is not None and self.proc.poll() is None

    def _scrape_identity(self) -> None:
        if self.identity is None and self.log_path.exists():
            m = _IDENT_RE.search(self.log_path.read_text(errors="replace"))
            if m:
                self.identity = m.group(1)

    async def wait_ready(self, timeout: float = 30.0) -> None:
        deadline = asyncio.get_event_loop().time() + timeout
        last = None
        while asyncio.get_event_loop().time() < deadline:
            if not self.alive():
                raise NodeError(f"{self.name}: astrald exited "
                                f"rc={self.proc.returncode}; see {self.log_path}")
            self._scrape_identity()
            if self.identity is not None:
                try:
                    async with await astral.connect(
                            self.endpoint, token=self.token,
                            connect_timeout=2.0) as c:
                        specs = await c.shell.spec()
                    if specs:
                        return
                except astral.AstralError as e:
                    last = e
            await asyncio.sleep(0.5)
        raise NodeError(f"{self.name}: not ready after {timeout}s "
                        f"(last error: {last}); see {self.log_path}")

    def stop(self) -> None:
        if self.proc is None or self.proc.poll() is not None:
            return
        self.proc.send_signal(signal.SIGINT)
        try:
            self.proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)
