#!/usr/bin/env python3
"""Driver: attach three watched directories to node1, then remove one.

`fs.new_watch` registers a directory as a read-only repository, indexes
it, and adds it to the `local` group. Unless `temporary` is set, it saves a
tree entry under `/mod/fs/repos`, and node1 restores every entry at start
(`addTreeRepos`, `mod/fs/src/deps.go`). `objects.remove_repository` drops a
repository; a watch repository's `AfterRemoved` removes its root from the
indexer and deletes its tree entry.

The roles:

- `kept`: persisted and never removed;
- `temp`: temporary, so the restart ends it;
- `gone`: persisted, then removed before the restart.

Each directory holds one file with a run-unique payload, written before the
watch. The three directories are siblings, so no root covers another: a
covered root keeps its rows when removed (`removeRoot`,
`mod/fs/src/indexer.go`).

The driver checks its preconditions (every file indexed and described at its
path) and records what node1 answers right after the removal; the oracle
judges it. The runner then restarts node1.
"""
import asyncio
import os
from pathlib import Path

import astral
from astral.errors import QueryRejected, RemoteError

from lib import fsfixture, nodefs
from lib.sessionio import load, write_facts

PREFIX = "fs-watch-restart"
TEMPORARY = {"kept": False, "temp": True, "gone": False}
INDEX_DEADLINE = 20.0


def names_and_local(infos) -> tuple[set, set]:
    names = {str(i.name) for i in infos}
    local = next((i for i in infos if str(i.name) == "local"), None)
    assert local is not None, "objects.repositories lists no `local` group"
    return names, {str(m) for m in local.children}


async def attach(c, n1: dict, role: str, stamp: str) -> dict:
    name = f"{PREFIX}-{role}"
    rel = f"{PREFIX}/{role}"
    nodefs.mkdir(n1, rel)
    payload = (f"{PREFIX} {role} in {stamp}: a file in the {role!r} "
               f"watched directory\n").encode()
    file_rel = f"{rel}/{role}.txt"
    nodefs.write_bytes(n1, file_rel, payload)

    directory = nodefs.path(n1, rel)
    err = await fsfixture.new_watch(c, directory, name,
                                    temporary=TEMPORARY[role])
    assert err is None, f"fs.new_watch {name}: {err}"
    return {"name": name, "temporary": TEMPORARY[role], "dir": directory,
            "file": nodefs.path(n1, file_rel), "file_rel": file_rel,
            "object_id": fsfixture.object_id(payload),
            "payload": payload.decode()}


async def check_indexed(c, r: dict, node1: str) -> None:
    missing = await fsfixture.wait_contains(c, r["name"], {r["object_id"]},
                                            INDEX_DEADLINE)
    assert not missing, (
        f"{r['name']} never indexed {r['file']} in {INDEX_DEADLINE:.0f}s")
    locs = await fsfixture.file_locations(c, r["object_id"])
    assert (r["file"], node1) in locs, (
        f"{r['name']}: {fsfixture.LOCATION} puts {r['object_id'][:16]}… at "
        f"{locs}, not at {r['file']} on node1")


async def after_removal(c, gone: dict) -> dict:
    """What node1 answers about `gone` right after its removal."""
    names, local = names_and_local(await c.objects.repositories())
    try:
        got = await c.objects.read(gone["object_id"])
        served = f"served {len(got)} B"
    except (QueryRejected, RemoteError):
        served = None
    return {
        "listed": gone["name"] in names,
        "in_local": gone["name"] in local,
        "local_contains": await c.objects.contains("local",
                                                   gone["object_id"]),
        "locations": [p for p, _ in await fsfixture.file_locations(
            c, gone["object_id"])],
        "default_read": served,
    }


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    stamp = session_dir.parent.name
    # why under node1's root: the watched directories must live on node1's
    # machine, which in a Docker world sees no host directory
    root = nodefs.path(n1, PREFIX)

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        repos = {role: await attach(c, n1, role, stamp)
                 for role in TEMPORARY}
        for r in repos.values():
            await check_indexed(c, r, n1["identity"])

        names, local = names_and_local(await c.objects.repositories())
        for r in repos.values():
            assert r["name"] in names and r["name"] in local, (
                f"{r['name']} is not listed in the local group after "
                f"fs.new_watch")

        gone = repos["gone"]
        await c.objects.remove_repository(gone["name"])
        gone["after_removal"] = await after_removal(c, gone)

    write_facts({
        "restarts_before": n1.get("restarts") or 0,
        "repos": repos,
    })
    print(f"driver: watched {', '.join(r['name'] for r in repos.values())} "
          f"under {root}; removed {gone['name']}: {gone['after_removal']}")


asyncio.run(main())
