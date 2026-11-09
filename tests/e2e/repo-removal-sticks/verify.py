#!/usr/bin/env python3
"""Oracle: after one restart, node1 restores the kept repository alone.

The runner restarts node1 between driver and oracle (test.toml `restart`),
and the session view counts the restart. Four claims:

- node1 was restarted exactly once since the driver ran;
- `objects.repositories` lists `kept`, and neither `gone` nor `temp`, and
  the `local` group names `kept` alone of the three;
- `kept` serves its object, by name and through the default read
  repository; `gone`'s and `temp`'s objects are answered with an in-band
  `error_message`, both by name ("repository not found") and through the
  default read repository;
- every file the driver stored is still on disk with its bytes: removing a
  repository and ending a temporary one never delete user files.

A removed repository that comes back is listed, joins `local` and serves its
object again, which fails the second and third claims.
"""
import asyncio
from pathlib import Path

import astral
from astral.errors import RemoteError

from lib.sessionio import as_bytes, load


async def refused(c, object_id: str, repo: str | None) -> str:
    """The error_message node1 answers a load with; fails when it serves bytes."""
    try:
        got = await c.objects.load(object_id, repo=repo)
    except RemoteError as e:
        return e.message
    where = f"repository {repo!r}" if repo else "the default read repository"
    raise AssertionError(
        f"{where} serves {object_id[:16]}… after the restart: "
        f"{as_bytes(got)!r}")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    repos = facts["repos"]
    kept, temp, gone = repos["kept"], repos["temp"], repos["gone"]

    restarts = (n1.get("restarts") or 0) - facts["restarts_before"]
    assert restarts == 1, (
        f"node1 was restarted {restarts} times between driver and oracle, "
        "not once")

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        infos = await c.objects.repositories()
        listed = {str(i.name) for i in infos}
        local = next((i for i in infos if str(i.name) == "local"), None)
        assert local is not None, "objects.repositories lists no `local` group"
        members = {str(m) for m in local.children}

        assert kept["name"] in listed, (
            f"{kept['name']} is not listed after the restart: the persisted "
            "repository was not restored")
        assert kept["name"] in members, (
            f"{kept['name']} is not in the local group after the restart")
        for r, why in ((gone, "a removed repository came back"),
                       (temp, "a temporary repository outlived the restart")):
            assert r["name"] not in listed, (
                f"{r['name']} is listed after the restart: {why}")
            assert r["name"] not in members, (
                f"{r['name']} is in the local group after the restart: {why}")

        payload = kept["payload"].encode()
        for repo in (kept["name"], None):
            got = as_bytes(await c.objects.load(kept["object_id"], repo=repo))
            where = repr(repo) if repo else "the default read repository"
            assert got == payload, (
                f"{where} serves {got!r} for {kept['object_id'][:16]}…, not "
                f"the {len(payload)} B stored in {kept['name']}")

        for r in (gone, temp):
            message = await refused(c, r["object_id"], r["name"])
            assert message == "repository not found", (
                f"a load from {r['name']!r} answered {message!r}, not "
                "'repository not found'")
            await refused(c, r["object_id"], None)

    for role, r in repos.items():
        file = Path(r["file"])
        assert file.is_file(), f"{role}: {file} is gone from disk"
        assert file.read_bytes() == r["payload"].encode(), (
            f"{role}: {file} no longer holds the bytes stored in it")

    print(f"oracle: after one restart node1 lists and serves {kept['name']} "
          f"alone of the three; {gone['name']} stays removed, {temp['name']} "
          "ended with the restart, and all three files are on disk")


asyncio.run(main())
