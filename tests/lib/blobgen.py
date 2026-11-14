"""Seeded bytes a node stores, and a paced reader that hashes them as it reads.

why a seed: an oracle regenerates the exact bytes from the seed alone, so it
judges a stream against bytes it made itself, never against the driver's copy.
SHAKE-256 is deterministic on every Python and host, and fast enough for tens
of MiB.

why running digests: a sha256 snapshot at every `step` boundary of the stream
names the first `step` where two streams differ, so a lost, duplicated or
reordered range shows as an offset, not only as a different final hash.
"""
import asyncio
import hashlib
import time

from astral.errors import QueryTimeout

MiB = 1024 * 1024


def blob(seed: str, size: int) -> bytes:
    """`size` bytes determined by `seed` alone."""
    return hashlib.shake_256(seed.encode()).digest(size)


def running_digests(data: bytes, step: int = MiB) -> list:
    """The sha256 hex of data[:k*step] for every k*step <= len(data)."""
    h = hashlib.sha256()
    out = []
    for start in range(0, len(data) - len(data) % step, step):
        h.update(data[start:start + step])
        out.append(h.hexdigest())
    return out


def first_difference(want: list, got: list) -> int | None:
    """The index of the first differing digest, len(shorter) when one is a
    prefix of the other and they differ in length, else None."""
    for i, (a, b) in enumerate(zip(want, got)):
        if a != b:
            return i
    if len(want) != len(got):
        return min(len(want), len(got))
    return None


async def write_object(c, data: bytes, *, target: str,
                       chunk: int = MiB) -> str:
    """Store data as one raw object on node `target`; its object id text.

    why create + write + commit: objects.store canonicalizes a typed object
    and refuses raw bytes (tests/e2e/object-store-peer).
    """
    async with c.objects.create(target=target) as w:
        for start in range(0, len(data), chunk):
            await w.write(data[start:start + chunk], timeout=60.0)
        async with asyncio.timeout(120.0):
            return str(await w.commit())


class PacedReader:
    """Reads a raw stream to EOF in `chunk`-byte reads, `pause` s apart.

    Records the byte count, the sha256 of everything read, a running digest at
    every `step`, and the longest gap between two reads that returned data.
    A read that returns nothing for `stall` s ends the reading with
    `stalled = True`; a read that raises ends it with `error`.

    `hold()` stops reading after the read in progress, `release()` reads on;
    the time held counts toward no gap.
    """

    def __init__(self, stream, *, chunk: int = 64 * 1024, pause: float = 0.008,
                 stall: float = 15.0, step: int = MiB):
        self.stream, self.chunk, self.pause = stream, chunk, pause
        self.stall, self.step = stall, step
        self.total = 0
        self.digests: list = []
        self.max_gap = 0.0
        self.stalled = False
        self.error: str | None = None
        self.eof = False
        self.done = asyncio.Event()
        self._h = hashlib.sha256()
        self._go = asyncio.Event()
        self._go.set()
        self._grew = asyncio.Event()

    @property
    def digest(self) -> str:
        return self._h.hexdigest()

    def hold(self) -> None:
        self._go.clear()

    def release(self) -> None:
        self._go.set()

    def _take(self, data: bytes) -> None:
        while data:
            room = self.step - self.total % self.step
            part, data = data[:room], data[room:]
            self._h.update(part)
            self.total += len(part)
            if self.total % self.step == 0:
                self.digests.append(self._h.hexdigest())

    async def run(self) -> None:
        last = time.monotonic()
        try:
            while True:
                if not self._go.is_set():
                    await self._go.wait()
                    last = time.monotonic()
                try:
                    data = await self.stream.read_bytes(self.chunk,
                                                        timeout=self.stall)
                except QueryTimeout:
                    self.stalled = True
                    return
                except Exception as e:  # noqa: BLE001 — recorded for the oracle
                    self.error = f"{type(e).__name__}: {e}"
                    return
                now = time.monotonic()
                if not data:
                    self.eof = True
                    return
                self.max_gap = max(self.max_gap, now - last)
                last = now
                self._take(data)
                self._grew.set()
                if self.pause:
                    await asyncio.sleep(self.pause)
        finally:
            self.done.set()
            self._grew.set()

    async def reached(self, offset: int, timeout: float) -> bool:
        """Wait until `offset` bytes have been read; False when the reading
        ended first or `timeout` s passed."""
        deadline = time.monotonic() + timeout
        while self.total < offset:
            if self.done.is_set():
                return False
            left = deadline - time.monotonic()
            if left <= 0:
                return False
            self._grew.clear()
            try:
                await asyncio.wait_for(self._grew.wait(), left)
            except (TimeoutError, asyncio.TimeoutError):
                return self.total >= offset
        return True

    def facts(self) -> dict:
        return {"total": self.total, "digest": self.digest,
                "digests": self.digests, "max_gap": round(self.max_gap, 3),
                "stalled": self.stalled, "error": self.error, "eof": self.eof}
