#!/usr/bin/env python3
"""Oracle: after one restart, node1 restores the persisted watch alone.

The runner restarts node1 between driver and oracle (test.toml `restart`).
Claims:

1. Right after its removal, before the restart, `gone` was unlisted, out of
   the `local` group, not contained in `local`, unread through the default
   read, and described at no path (the driver recorded node1's answers).
2. node1 was restarted exactly once since the driver ran.
3. `objects.repositories` lists `kept` and neither `temp` nor `gone`; the
   `local` group names `kept` alone of the three.
4. Within 20 s `kept` contains its file's id, serves its bytes by name, and
   `mod.fs.file_location` places it at its path on node1. The id is the hash
   of the payload, computed here.
5. A load of `temp`'s or `gone`'s id by repository name answers the in-band
   "repository not found", and a read through the default read is refused.
6. Within 20 s `objects.describe` gives no file location for `temp`'s or
   `gone`'s id: the restart's rescan drops rows under no registered root.
7. All three files are still on disk with their bytes.
"""
import asyncio

import astral
from astral.errors import RemoteError

from lib import fsfixture, nodefs
from lib.sessionio import as_bytes, load

DEADLINE = 20.0
INTERVAL = 0.5


def check_removal(gone: dict) -> None:
    """Claim 1."""
    seen = gone["after_removal"]
    want = {"listed": False, "in_local": False, "local_contains": False,
            "locations": [], "default_read": None}
    assert seen == want, (
        f"right after objects.remove_repository {gone['name']}, node1 "
        f"answered {seen}, not {want}")


async def poll(check, what: str):
    """Run `check` until it answers a falsy failure text, for DEADLINE s."""
    loop = asyncio.get_running_loop()
    end = loop.time() + DEADLINE
    while True:
        failure = await check()
        if not failure:
            return
        assert loop.time() < end, f"{what} after {DEADLINE:.0f}s: {failure}"
        await asyncio.sleep(INTERVAL)


async def check_listing(c, kept: dict, others: list) -> None:
    """Claim 3."""
    infos = await c.objects.repositories()
    listed = {str(i.name) for i in infos}
    local = next((i for i in infos if str(i.name) == "local"), None)
    assert local is not None, "objects.repositories lists no `local` group"
    members = {str(m) for m in local.children}
    assert kept["name"] in listed and kept["name"] in members, (
        f"{kept['name']} is not listed in the local group after the restart: "
        f"the persisted watch was not restored")
    for r, why in others:
        assert r["name"] not in listed and r["name"] not in members, (
            f"{r['name']} is listed after the restart: {why}")


async def check_kept(c, kept: dict, node1: str) -> None:
    """Claim 4."""
    oid = fsfixture.object_id(kept["payload"].encode())
    assert oid == kept["object_id"], "the facts' id is not the payload's"

    async def served():
        if not await c.objects.contains(kept["name"], oid):
            return f"{kept['name']} does not contain {oid[:16]}…"
        got = await c.objects.read(oid, repo=kept["name"])
        if got != kept["payload"].encode():
            return f"{kept['name']} serves {got!r}"
        locs = await fsfixture.file_locations(c, oid)
        if (kept["file"], node1) not in locs:
            return f"{fsfixture.LOCATION} places it at {locs}"
        return None

    await poll(served, f"the restored {kept['name']} does not serve "
                       f"{kept['file']}")


async def check_unserved(c, r: dict) -> None:
    """Claim 5."""
    try:
        got = await c.objects.load(r["object_id"], repo=r["name"])
    except RemoteError as e:
        assert e.message == "repository not found", (
            f"a load from {r['name']!r} answered {e.message!r}, not "
            f"'repository not found'")
    else:
        raise AssertionError(f"{r['name']!r} serves {as_bytes(got)!r} after "
                             f"the restart")
    served = await fsfixture.read_refused(c, r["object_id"], None)
    assert served is None, (
        f"the default read {served} for {r['object_id'][:16]}… of "
        f"{r['name']} after the restart")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    repos = facts["repos"]
    kept, temp, gone = repos["kept"], repos["temp"], repos["gone"]

    check_removal(gone)

    restarts = (n1.get("restarts") or 0) - facts["restarts_before"]
    assert restarts == 1, (
        f"node1 was restarted {restarts} times between driver and oracle, "
        "not once")

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        await check_listing(c, kept, [
            (gone, "a removed watch came back"),
            (temp, "a temporary watch outlived the restart")])
        await check_kept(c, kept, n1["identity"])
        for r in (temp, gone):
            await check_unserved(c, r)

            async def undescribed(r=r):
                locs = await fsfixture.file_locations(c, r["object_id"])
                return f"described at {locs}" if locs else None

            await poll(undescribed, f"{r['name']}'s file is still described")

    for role, r in repos.items():
        file, rel = r["file"], r["file_rel"]
        parent, _, base = rel.rpartition("/")
        assert (nodefs.listdir(n1, parent).get(base) == "file"
                and nodefs.read_bytes(n1, rel) == r["payload"].encode()), (
            f"{role}: {file} is gone or no longer holds its bytes")

    print(f"oracle: after one restart node1 lists and serves {kept['name']} "
          f"alone of the three; {gone['name']} stays removed, {temp['name']} "
          f"ended with the restart, neither file is described, and all three "
          f"files are on disk")


asyncio.run(main())
