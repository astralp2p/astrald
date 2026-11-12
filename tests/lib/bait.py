"""A TCP listener on the host that accepts and then says nothing.

A node that dials it gets a connection, sends its first handshake bytes, and
hears nothing back. The bait reads and discards what arrives, never writes,
and records each connection, so a test sees whether, and when, the node let
go of it.

    async with Bait() as bait:
        endpoint = f"tcp:127.0.0.1:{bait.port}"
        ...
        table = bait.table()

A bait serves on the host, so its test is local_only.
"""
import asyncio
import time


class Bait:
    """Records per connection: accepted_at, bytes_in, eof_at (host clock,
    time.time()); eof_at is None while the peer holds it open."""

    def __init__(self, host: str = "127.0.0.1", port: int = 0):
        self.host, self.port = host, port
        self.conns: list = []
        self._server = None
        self._writers: list = []

    async def __aenter__(self) -> "Bait":
        self._server = await asyncio.start_server(self._serve, self.host,
                                                  self.port)
        self.port = self._server.sockets[0].getsockname()[1]
        return self

    async def __aexit__(self, *exc) -> None:
        self._server.close()
        for w in self._writers:
            w.close()
        await self._server.wait_closed()

    async def _serve(self, reader, writer) -> None:
        rec = {"accepted_at": time.time(), "bytes_in": 0, "eof_at": None,
               "peer": "%s:%s" % writer.get_extra_info("peername")[:2]}
        self.conns.append(rec)
        self._writers.append(writer)
        try:
            while True:
                chunk = await reader.read(65536)
                if not chunk:
                    break
                rec["bytes_in"] += len(chunk)
        except (ConnectionError, OSError):
            pass
        rec["eof_at"] = time.time()

    def open(self) -> int:
        """How many accepted connections the peer still holds open."""
        return sum(1 for c in self.conns if c["eof_at"] is None)

    def table(self) -> list:
        return [dict(c) for c in self.conns]
