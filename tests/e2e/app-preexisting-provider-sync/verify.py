#!/usr/bin/env python3
"""Oracle: the provider registered before the adoption is reachable after it.

Judges the recorded steps and the live state of both nodes: the joiner was
outside the swarm when the provider registered and is a linked member now, and
the User's node holds the provider's relay contract for the joiner. The
provider is stopped by then, so the story is not replayed.
"""
import asyncio

import astral

from lib import jsonops
from lib.sessionio import load

STEPS = ["discovered_after_adoption", "provider_echoed", "user_node_knows_the_host"]


async def main():
    doc = load()
    home, joiner = doc["nodes"]["ppsyncuser"], doc["nodes"]["ppsyncjoiner"]
    facts = doc["facts"].get("app_preexisting_provider_sync", {})

    missing = [s for s in STEPS if not facts.get(s)]
    assert not missing, (f"steps that did not hold: {missing}; "
                         f"the consumer said: {facts.get('consumer_error')}")
    assert facts.get("joiner_state_before") == "fresh", (
        f"the joiner was {facts.get('joiner_state_before')!r} when the provider "
        "registered; want fresh (outside any swarm)")
    assert facts.get("joiner_state_after") == "claimed", (
        f"the joiner was {facts.get('joiner_state_after')!r} after the adoption; want claimed")
    served = facts.get("provider_served")
    assert served == [facts["consumer"]], (
        f"the provider served {served}; want the consumer {facts['consumer']} alone")

    # live: the joiner is a linked member of the User's swarm
    async with await astral.connect(home["endpoint"], token=home["token"]) as c:
        members = {str(m.identity): m for m in await c.user.swarm_status()}
        m = members.get(joiner["identity"])
        assert m is not None and m.linked, (
            f"the joiner {joiner['identity']} is not a linked member: {sorted(members)}")
        info = await c.user.info()
        assert str(info.user_id) == facts["user_id"], "the User's node answers for another User"

    # live: the User's node names the joiner as the host of the provider app,
    # which it learns only from the provider's relay contract
    async with await astral.connect(home["endpoint"], token=home["token"]) as c:
        docs = await jsonops.call(c, f"apphost.hosts?app={facts['provider']}&out=json")
    hosts = [d["Object"] for d in docs if d["Type"] == "identity"]
    assert hosts == [joiner["identity"]], (
        f"apphost.hosts on the User's node named {hosts} for the provider; "
        f"want the joiner {joiner['identity']} alone: {docs}")

    # live: the joiner is in the User's swarm under the same User
    async with await astral.connect(joiner["endpoint"], token=joiner["token"]) as c:
        info = await c.user.info()
        assert str(info.user_id) == facts["user_id"], (
            f"the joiner answers for User {info.user_id}; want {facts['user_id']}")

    print("oracle: the provider registered on the joiner before its adoption was "
          "discovered with reach=swarm, called, and is hosted by the joiner")


asyncio.run(main())
