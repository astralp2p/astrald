#!/usr/bin/env python3
"""Oracle: node1's rebuilt auth index leaves out the expelled host's app.

node1 rebuilt its auth index from its repository at start-up. The indexer
admits a relay contract through the user module's rule (astrald
mod/user/src/relay_admission.go AdmitRelayContract, registered with
Auth.AddAdmitter in mod/user/src/deps.go): its subject must be node1 itself or
a member of the local swarm, and node2 is expelled (astral-docs
topics/app-routing.md, Reach). So within 30 s:

- apphost.hosts on node1 names node1 for node1's own app: the index was
  rebuilt, so the absence below is a refusal, not an indexer that never ran;
- apphost.hosts on node1 names no host for node2's app.
"""
import asyncio
import time

import astral

from lib import jsonops
from lib.sessionio import load

CONVERGE = 30.0


async def hosts(c, app: str) -> list:
    docs = await jsonops.call(c, f"apphost.hosts?app={app}&out=json")
    return [d["Object"] for d in docs if d["Type"] == "identity"]


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]["reindex_after_expel"]

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        deadline = time.monotonic() + CONVERGE
        local = await hosts(c, facts["local_app"])
        while n1["identity"] not in local and time.monotonic() < deadline:
            await asyncio.sleep(0.5)
            local = await hosts(c, facts["local_app"])
        remote = await hosts(c, facts["remote_app"])

    assert local == [n1["identity"]], (
        f"node1 did not re-index its own app's contract within {CONVERGE:.0f} s: "
        f"hosts {local}")
    assert remote == [], (
        f"node1 re-indexed the expelled node2's app contract: hosts {remote}")

    print("oracle: node1 re-indexed its own app and left out the expelled "
          f"node2's app (before the wipe: {facts['hosts_after_expel_before_wipe']})")


asyncio.run(main())
