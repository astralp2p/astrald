"""A node's own log as log.listen streams it: a driver records it, an oracle reads it.

why the log: some of what astrald does leaves no state an op can read. A link
task cutting its backoff short, or nearby switching its mode, shows only in
the node's log, and in a VM world a guest's journal reaches the host only when
the world closes (lib/worlds/vm.py _pull_logs), after every oracle has run.

why log.listen: it streams every entry the node logs while the query is open,
whatever the console's verbosity, since a subscriber is not filtered
(astral-go astral/log/logger.go, logEntry), to a caller the node lets see its
state (mod/log/src/op_listen.go, SeeNodeStateAction). The node's own token is
such a caller. A driver keeps the entries it needs as facts, and the oracle
judges those.

why each entry keeps its node's clock: an entry carries the time its node
logged it, so an interval between two entries of one node needs no clock the
host or another guest shares.
"""
import asyncio
import json
import re
from contextlib import AsyncExitStack, suppress
from datetime import datetime

import astral

QUERY = "log.listen?out=json"
ENTRY = "astrald.log.entry"
TAG = "astrald.log.tag"
_FRACTION = re.compile(r"(\.\d{6})\d+")


def epoch(rfc3339: str) -> float:
    """Seconds since the epoch of an RFC 3339 time, as astral.Time writes one.

    why the trim: astral.Time writes nanoseconds, and datetime parses six
    fractional digits at most.
    """
    return datetime.fromisoformat(_FRACTION.sub(r"\1", rfc3339)).timestamp()


def render(entry: dict) -> dict:
    """One astrald.log.entry as {t, level, tags, text}.

    `text` joins the entry's objects as the console prints them, an identity
    as its hex form rather than its alias; `tags` are the prefixes its logger
    set, the module name among them.
    """
    tags, parts = [], []
    for obj in entry.get("Objects") or []:
        if isinstance(obj, dict) and "Type" in obj:
            kind, value = obj["Type"], obj.get("Object")
        else:
            kind, value = None, obj
        if kind == TAG:
            tags.append(str(value))
            continue
        parts.append(value if isinstance(value, str) else json.dumps(value))
    return {"t": epoch(entry["Time"]), "level": entry.get("Level"),
            "tags": tags, "text": "".join(parts)}


class LogTap:
    """Every entry one node logs while the tap is open, in order, rendered.

        async with LogTap(node) as tap:
            ...                       # the flow
        lines = tap.find("restored after")

    `refused` holds the node's answer when it refused the query, and `broken`
    what ended the stream early; either means the record is incomplete.
    """

    def __init__(self, node: dict):
        self.node = node
        self.entries: list = []
        self.refused: str | None = None
        self.broken: str | None = None
        self._stack: AsyncExitStack | None = None
        self._task: asyncio.Task | None = None
        self._stream = None

    async def __aenter__(self) -> "LogTap":
        self._stack = AsyncExitStack()
        client = await self._stack.enter_async_context(
            await astral.connect(self.node["endpoint"], token=self.node["token"]))
        # why persistent and no deadline: the stream never ends on its own and
        # stays quiet while the node logs nothing (astral-py Client.follow)
        self._stream = await self._stack.enter_async_context(
            client.stream(QUERY, raw=True, persistent=True, timeout=None))
        self._task = asyncio.create_task(self._pump())
        return self

    async def __aexit__(self, *exc) -> None:
        if self._task is not None:
            self._task.cancel()
            with suppress(asyncio.CancelledError):
                await self._task
        await self._stack.aclose()

    async def _pump(self) -> None:
        buf = b""
        try:
            while True:
                chunk = await self._stream.read_bytes(65536)
                if not chunk:
                    self.broken = self.broken or "the node ended log.listen"
                    return
                buf += chunk
                *lines, buf = buf.split(b"\n")
                for line in lines:
                    if line.strip():
                        self._take(json.loads(line))
        except asyncio.CancelledError:
            raise
        except Exception as e:                    # noqa: BLE001
            self.broken = f"{type(e).__name__}: {e}"

    def _take(self, doc: dict) -> None:
        if doc.get("Type") == "error_message":
            self.refused = str(doc.get("Object"))
        elif doc.get("Type") == ENTRY:
            self.entries.append(render(doc["Object"]))

    def find(self, *needles: str) -> list:
        """The entries whose text holds any of needles, in order."""
        return [e for e in self.entries if any(n in e["text"] for n in needles)]

    def state(self) -> dict:
        """Whether the record is whole, as a fact."""
        return {"refused": self.refused, "broken": self.broken,
                "entries": len(self.entries)}
