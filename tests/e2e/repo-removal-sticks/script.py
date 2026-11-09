#!/usr/bin/env python3
"""Driver: attach three directory repositories to node1, then remove one.

`fs.new_repo` registers a host directory as a writable repository and adds
it to the `local` group. A repository created without `temporary` gets a tree
entry under `/mod/fs/repos`, and node1 restores every entry at start. A
`temporary` repository gets no entry. `objects.remove_repository` drops a
repository, and the fs repository's `AfterRemoved` deletes its tree entry.

The roles:

- `kept`: persisted and never removed;
- `temp`: temporary, so the restart ends it;
- `gone`: persisted, then removed before the restart.

Each repository holds one file, a run-unique payload stored through
`objects.create`. The driver checks every precondition the oracle relies on:
the three repositories are listed and serve their objects before the removal,
and `gone` is unlisted and unserved right after it. The runner then restarts
node1, and the oracle judges what the restart restored.

The directories live under the test's own results directory, never elsewhere
on the host.
"""
import asyncio
import os
from pathlib import Path

import astral
from astral import querystring
from astral.errors import RemoteError

from lib.sessionio import as_bytes, load, write_facts

PREFIX = "repo-removal-sticks"
TEMPORARY = {"kept": False, "temp": True, "gone": False}


def names_of(infos) -> set:
    return {str(i.name) for i in infos}


def local_children(infos) -> set:
    for i in infos:
        if str(i.name) == "local":
            return {str(c) for c in i.children}
    raise AssertionError("objects.repositories lists no `local` group")


async def attach(c, role: str, root: Path, stamp: str) -> dict:
    name = f"{PREFIX}-{role}"
    directory = root / role
    directory.mkdir(parents=True, exist_ok=True)

    params = {"path": str(directory), "name": name}
    if TEMPORARY[role]:
        params["temporary"] = True
    ack = await c.call_one(querystring.build("fs.new_repo", params))
    assert type(ack).__name__ == "Ack", f"fs.new_repo {name}: answered {ack!r}"

    payload = (f"{PREFIX} {role} in {stamp}: a file stored in the "
               f"{role!r} repository\n").encode()
    async with c.objects.create(repo=name) as w:
        await w.write(payload)
        object_id = str(await w.commit())

    file = directory / object_id
    assert file.read_bytes() == payload, (
        f"{name}: {file} does not hold the {len(payload)} B just stored")

    got = as_bytes(await c.objects.load(object_id, repo=name))
    assert got == payload, f"{name} serves {got!r} for {object_id[:16]}…"

    return {
        "name": name,
        "temporary": TEMPORARY[role],
        "dir": str(directory),
        "file": str(file),
        "object_id": object_id,
        "payload": payload.decode(),
    }


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    stamp = session_dir.parent.name  # results/<stamp>/<test>/session.json
    root = session_dir / PREFIX

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        repos = {role: await attach(c, role, root, stamp) for role in TEMPORARY}

        infos = await c.objects.repositories()
        for role, r in repos.items():
            assert r["name"] in names_of(infos), (
                f"{r['name']} is not listed after fs.new_repo")
            assert r["name"] in local_children(infos), (
                f"{r['name']} is not in the local group after fs.new_repo")

        gone = repos["gone"]
        await c.objects.remove_repository(gone["name"])

        infos = await c.objects.repositories()
        assert gone["name"] not in names_of(infos), (
            f"{gone['name']} is still listed right after its removal")
        assert gone["name"] not in local_children(infos), (
            f"{gone['name']} is still in the local group right after its removal")
        try:
            got = await c.objects.load(gone["object_id"])
        except RemoteError:
            pass
        else:
            raise AssertionError(
                f"{gone['object_id'][:16]}… is still served right after "
                f"{gone['name']} was removed: {as_bytes(got)!r}")

    write_facts({
        "restarts_before": n1.get("restarts") or 0,
        "repos": repos,
    })
    print(f"driver: attached {', '.join(r['name'] for r in repos.values())} "
          f"under {root}; removed {gone['name']}")


asyncio.run(main())
