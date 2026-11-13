"""Directories on node1's machine: the fs ops, the expected ids, and the file
locations node1 reports.

astral-py binds no `fs` module, so `fs.new_watch` and `fs.new_repo` are raw
queries built with `querystring.build`, which encodes the path. Both answer an
`astral.Ack`, or an in-band `error_message` that astral-py raises as
`RemoteError`; the wrappers answer None for the Ack and the error text
otherwise, so a test asserts a refusal as plainly as a success.

`disk_ids` is the oracle's own expected set: it walks a directory under the
node's root through lib/nodefs.py, keeps the regular files (a symlink is never
one, whatever it points at), reads their bytes itself and hashes them with
astral-py's `object_id_of_bytes`. A test compares what node1 lists against it,
never against ids node1 reported earlier.
"""
import asyncio
import posixpath

from astral import querystring
from astral.errors import QueryRejected, RemoteError
from astral.objectid import object_id_of_bytes

from lib import nodefs

LOCATION = "mod.fs.file_location"


def object_id(data: bytes) -> str:
    """The id of untyped bytes, as node1 spells it."""
    return str(object_id_of_bytes(data))


def disk_ids(node: dict, rel: str) -> dict[str, str]:
    """{path: id} for every regular file under the directory `rel` of the
    node's root, each path as the node's machine sees it (nodefs.path).
    Symlinks, to files or directories, are skipped and not followed."""
    out = {}
    for name, kind in nodefs.listdir(node, rel).items():
        sub = posixpath.join(rel, name)
        if kind == "dir":
            out |= disk_ids(node, sub)
        elif kind == "file":
            out[nodefs.path(node, sub)] = object_id(
                nodefs.read_bytes(node, sub))
    return out


async def _attach(c, op: str, path, name: str, temporary: bool) -> str | None:
    params = {"path": str(path), "name": name}
    if temporary:
        params["temporary"] = True
    try:
        got = await c.call_one(querystring.build(op, params))
    except RemoteError as e:
        return e.message
    if type(got).__name__ == "Ack":
        return None
    return f"{op} answered {got!r}, not an ack"


async def new_watch(c, path, name: str, temporary: bool = False) -> str | None:
    """`fs.new_watch`: None when node1 acks, else the error text."""
    return await _attach(c, "fs.new_watch", path, name, temporary)


async def new_repo(c, path, name: str, temporary: bool = False) -> str | None:
    """`fs.new_repo`: None when node1 acks, else the error text."""
    return await _attach(c, "fs.new_repo", path, name, temporary)


async def wait_scan(c, repo: str, want: set, deadline: float,
                    interval: float = 0.5) -> tuple[bool, set]:
    """Poll `objects.scan(repo)` until it lists exactly `want`, for up to
    `deadline` seconds. Answers whether it did, and the last listing."""
    loop = asyncio.get_running_loop()
    end = loop.time() + deadline
    while True:
        got = {str(i) for i in await c.objects.scan(repo)}
        if got == want or loop.time() >= end:
            return got == want, got
        await asyncio.sleep(interval)


async def wait_contains(c, repo: str, ids, deadline: float,
                        interval: float = 0.5) -> set:
    """Poll `objects.contains` until `repo` holds every id in `ids`, for up to
    `deadline` seconds. Answers the ids still missing."""
    loop = asyncio.get_running_loop()
    end = loop.time() + deadline
    missing = set(ids)
    while True:
        missing = {i for i in missing if not await c.objects.contains(repo, i)}
        if not missing or loop.time() >= end:
            return missing
        await asyncio.sleep(interval)


def scan_diff(want: set, got: set) -> str:
    """What a listing lacks and what it holds besides, for a failure message."""
    return (f"missing {sorted(i[:16] for i in want - got)}, "
            f"extra {sorted(i[:16] for i in got - want)}")


async def file_locations(c, oid: str) -> list[tuple[str, str]]:
    """[(Path, NodeID)] of every `mod.fs.file_location` node1 describes `oid`
    with. The type is learned from node1 first, since astral-py was never
    built against it."""
    await c.objects.learn(LOCATION)
    return [(str(d.data.get("Path")), str(d.data.get("NodeID")))
            for d in await c.objects.describe(oid)
            if getattr(d.data, "ASTRAL_TYPE", "") == LOCATION]


async def read_refused(c, oid: str, repo: str | None) -> str | None:
    """None when node1 refuses to read `oid` (a query rejection or an
    in-band error), else a description of the bytes it served."""
    try:
        got = await c.objects.read(oid, repo=repo)
    except (QueryRejected, RemoteError):
        return None
    return f"served {len(got)} B"
