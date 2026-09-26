"""Ops driven over apphost with JSON on both sides of the channel.

why JSON and not astral-py's codecs: astral-py registers no messaging types,
and the {Type, Object} envelope is the node's own lossless text form of every
object it knows, so a driver reads and writes those types with no codec of its
own. The query string names `out=json`, and `in=json` for an op that reads a
body.

why the stream is raw: astral-py's framed reader decodes each object with its
own codecs, and a raw read hands back the JSON the node wrote. An error_message
therefore arrives as one more envelope, and the caller decides what it means.
"""
import json

EOS = {"Type": "eos", "Object": None}


class OpError(Exception):
    """The op accepted the query and answered an error_message."""


async def call(client, qs: str, body: dict | None = None, eos: bool = False,
               **kw) -> list:
    """Every envelope an op answered, in order, `eos` included.

    `body` is sent as one envelope after the query is accepted. `eos` follows
    it for a batch op, which reads its input to one.
    """
    async with client.stream(qs, raw=True, **kw) as s:
        for doc in ([body] if body is not None else []) + ([EOS] if eos else []):
            await s.send_bytes((json.dumps(doc) + "\n").encode())
        raw = await s.read_bytes()
    return [json.loads(line) for line in raw.decode().splitlines()
            if line.strip()]


async def value(client, qs: str, body: dict | None = None, **kw):
    """The one object an op answered. An error_message raises OpError."""
    docs = await call(client, qs, body, **kw)
    if not docs:
        raise OpError(f"{qs}: the op answered nothing")
    if docs[0]["Type"] == "error_message":
        raise OpError(docs[0]["Object"])
    return docs[0]["Object"]


async def stream(client, qs: str, **kw) -> list:
    """The objects a streaming op answered before its `eos`. An error_message
    raises OpError."""
    out = []
    for doc in await call(client, qs, **kw):
        if doc["Type"] == "error_message":
            raise OpError(doc["Object"])
        if doc["Type"] == "eos":
            break
        out.append(doc)
    return out
