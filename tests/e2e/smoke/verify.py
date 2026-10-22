#!/usr/bin/env python3
"""Oracle: the node answers, confines a token-less session, authenticates, and
exposes the core ops."""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    smoke = doc["facts"]["smoke"]

    # user.info answers a claimed node and rejects with code 2 on an unclaimed
    # one; either way the query reached the op.
    info = smoke["user_info"]
    assert not info["refused"] or "code 2" in info["detail"], (
        f"anonymous user.info: {info['detail']} — the claim-state probe is not "
        "open to a token-less session")

    spec = smoke["shell_spec"]
    assert spec["refused"] and spec["detail"].startswith("Denied"), (
        f"anonymous shell.spec: {spec['detail']} — a token-less session "
        "reached an op off the anonymous allowlist")

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        assert c.authenticated, "node token not accepted"
        names = {s.name for s in await c.shell.spec()}
        who = await c.apphost.whoami()
    for op in ("user.info", "dir.set_alias", "nodes.add_endpoint"):
        assert op in names, f"missing op {op}"
    assert str(who) == n1["identity"], (
        f"whoami {who} != node identity {n1['identity']}")
    print("oracle: anonymous user.info reached, shell.spec denied; spec + auth + whoami ok")


asyncio.run(main())
