"""`indexing.subscribe` and its sibling ops, driven as JSON.

astral-py binds no `indexing` module, so the ops are raw queries with
`in=json&out=json`, and every object travels as the node's `{Type, Object}`
envelope (lib/jsonops.py gives the reasons). `indexing.subscribe` is a
two-way stream: the node sends one `indexing.index` or `indexing.unindex`
change, then waits for an `indexing.ack` naming the same repository and
version before it commits the cursor and sends the next change.
"""
import asyncio
import json

from astral.errors import QueryTimeout

from lib import jsonops

INDEX = "indexing.index"
UNINDEX = "indexing.unindex"
ERROR = "error_message"

# why: the node answers an astral.Err with this text when a nonce is unknown
# or, by design, not the caller's (op_subscribe.go, op_unregister_indexer.go)
INDEX_NOT_FOUND = "index not found"


def change(doc: dict) -> dict:
    """An envelope reduced to what the oracle compares: its type and, for a
    change, the repository, version and object it names."""
    obj = doc.get("Object")
    out = {"type": doc.get("Type")}
    if out["type"] in (INDEX, UNINDEX):
        out.update(repo=obj["Repo"], version=int(obj["Version"]),
                   object=str(obj["ObjectID"]))
    else:
        out["object"] = obj
    return out


class Subscription:
    """One `indexing.subscribe` query, read one envelope at a time.

    The stream is raw, so lines are split here. A read that times out leaves
    the stream usable; the end of the stream reads as None.
    """

    def __init__(self, client, nonce: str):
        self._ctx = client.stream(
            f"indexing.subscribe?nonce={nonce}&in=json&out=json",
            raw=True, persistent=True, timeout=None)
        self._buf = b""
        self._eof = False
        self.s = None

    async def __aenter__(self):
        self.s = await self._ctx.__aenter__()
        return self

    async def __aexit__(self, *exc):
        return await self._ctx.__aexit__(*exc)

    async def next(self, timeout: float) -> dict | None:
        """The next envelope, None at the end of the stream; raises
        TimeoutError when nothing arrives within `timeout` seconds."""
        loop = asyncio.get_running_loop()
        end = loop.time() + timeout
        while b"\n" not in self._buf:
            if self._eof:
                return None
            left = end - loop.time()
            if left <= 0:
                raise TimeoutError
            try:
                data = await self.s.read_bytes(4096, timeout=left)
            except QueryTimeout as e:
                raise TimeoutError from e
            if not data:
                self._eof = True
                if not self._buf.strip():
                    return None
                self._buf += b"\n"
            self._buf += data
        line, self._buf = self._buf.split(b"\n", 1)
        if not line.strip():
            return await self.next(max(end - loop.time(), 0.001))
        return json.loads(line)

    async def ack(self, c: dict) -> None:
        """Acknowledge change `c`, as `change()` reduced it."""
        doc = {"Type": "indexing.ack",
               "Object": {"Repo": c["repo"], "Version": c["version"]}}
        await self.s.send_bytes((json.dumps(doc) + "\n").encode())


async def drain(client, nonce: str, idle: float) -> list[dict]:
    """Subscribe, acknowledge every change, and stop once none has arrived for
    `idle` seconds. Answers the envelopes seen, each reduced by `change()`."""
    seen = []
    async with Subscription(client, nonce) as sub:
        while True:
            try:
                doc = await sub.next(idle)
            except TimeoutError:
                return seen
            if doc is None:
                return seen
            c = change(doc)
            seen.append(c)
            if c["type"] not in (INDEX, UNINDEX):
                return seen
            await sub.ack(c)


async def register_indexer(client, name: str) -> str:
    """The nonce `indexing.register_indexer` answers for `name`."""
    return await jsonops.value(
        client, f"indexing.register_indexer?name={name}&in=json&out=json")


async def answer(client, qs: str) -> dict:
    """The first envelope a one-shot op answered, reduced by `change()`."""
    docs = await jsonops.call(client, qs)
    if not docs:
        return {"type": None, "object": None}
    return change(docs[0])
